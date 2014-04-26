package main

/*
 * This file is part of theary.
 * 
 * It uses portion of code from Go-Guerrilla SMTPd
 * Copyright (c) 2012 Flashmob, GuerrillaMail.com
 *
 * theary is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * theary is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with Foobar.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sloonz/go-qprintable"
	
	//TODO : replace this C binding "github.com/sloonz/go-iconv"
	// By a pure go solution with go.text
	//"code.google.com/p/go.text/encoding"
	//"code.google.com/p/go.text/encoding/charmap"
	//"code.google.com/p/go.text/transform"
	
	"io"
	"io/ioutil"
	"log"
	"net"
	
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	
	"github.com/HouzuoGuo/tiedot/db"
	randomize "math/rand"
	"bitbucket.org/kardianos/osext"
	"bitbucket.org/kardianos/service"
	"path/filepath"
)

type Client struct {
	state       int
	Helo        string
	Mail_from   string
	Rcpt_to     string
	read_buffer string
	Response    string
	Address     string
	Data        string
	Subject     string
	Hash        string
	time        int64
	tls_on      bool
	conn        net.Conn
	bufin       *bufio.Reader
	bufout      *bufio.Writer
	kill_time   int64
	errors      int
	ClientId    int64
	savedNotify chan int
}

var TLSconfig *tls.Config
var max_size int // max email Data size
var timeout time.Duration
var allowedHosts = make(map[string]bool, 15)
var sem chan int              // currently active clients
var SaveMailChan chan *Client // workers for saving mail

// defaults. Overwrite any of these in the configure() function which loads them from a json file
var gConfig = make(map[string]string)
var exePath, logFile, configFile, dataPath, tmplPath, staticPath string
var dbEmails *db.DB

var logSrv service.Logger
var name = "theary"
var displayName = "fake SMTP Server"
var desc = "fake SMTP Server with a minimalist webmail client written in pure go"
var isService bool = true

func logln(level int, s string) {
	if gConfig["GSMTP_VERBOSE"] == "Y" {
		fmt.Println(s)
	}
	if level == 2 {
		log.Fatalf(s)
	}
	if len(gConfig["GSMTP_LOG_FILE"]) > 0 {
		log.Println(s)
	}
}

// main runs the program as a service or as a command line tool.
// Several verbs allows you to install, start, stop or remove the service.
// "run" verb allows you to run the program as a command line tool.
// e.g. "theary install" installs the service
// e.g. "theary run" starts the program from the console (blocking)
func main() {
	s, err := service.NewService(name, displayName, desc)
	if err != nil {
		fmt.Printf("%s unable to start: %s", displayName, err)
		return
	}
	logSrv = s

	if len(os.Args) > 1 {
		var err error
		verb := os.Args[1]
		switch verb {
		case "install":
			err = s.Install()
			if err != nil {
				fmt.Printf("Failed to install: %s\n", err)
				return
			}
			fmt.Printf("Service \"%s\" installed.\n", displayName)
		case "remove":
			err = s.Remove()
			if err != nil {
				fmt.Printf("Failed to remove: %s\n", err)
				return
			}
			fmt.Printf("Service \"%s\" removed.\n", displayName)
		case "run":
			isService = false
			doWork()
		case "start":
			err = s.Start()
			if err != nil {
				fmt.Printf("Failed to start: %s\n", err)
				return
			}
			fmt.Printf("Service \"%s\" started.\n", displayName)
		case "stop":
			err = s.Stop()
			if err != nil {
				fmt.Printf("Failed to stop: %s\n", err)
				return
			}
			fmt.Printf("Service \"%s\" stopped.\n", displayName)
		}
		return
	}
	err = s.Run(func() error {
		// start
		go doWork()
		return nil
	}, func() error {
		// stop
		stopWork()
		return nil
	})
	if err != nil {
		s.Error(err.Error())
	}
}

// configure sets up theary by reading the configuration file
func configure() {

	exePath, _ = osext.ExecutableFolder()
	configFile = filepath.Join(exePath, "conf", "conf.json")
	logFile = filepath.Join(exePath, "logs", "theary.log")
	dataPath = filepath.Join(exePath, "data")
	tmplPath = filepath.Join(exePath, "tmpl")
	staticPath = filepath.Join(exePath, "static")

	f, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()
	log.SetOutput(f)
	log.Println("config file", configFile)

	// load in the config.
	b, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Fatalln("Could not read config file")
	}
	var myConfig map[string]string
	err = json.Unmarshal(b, &myConfig)
	if err != nil {
		log.Fatalln("Could not parse config file")
	}
	for k, v := range myConfig {
		gConfig[k] = v
	}
	// map the allow hosts for easy lookup
	if arr := strings.Split(gConfig["GM_ALLOWED_HOSTS"], ","); len(arr) > 0 {
		for i := 0; i < len(arr); i++ {
			allowedHosts[arr[i]] = true
		}
	}
	var n int
	var n_err error
	// sem is an active clients channel used for counting clients
	if n, n_err = strconv.Atoi(gConfig["GM_MAX_CLIENTS"]); n_err != nil {
		n = 50
	}
	// currently active client list
	sem = make(chan int, n)
	// Database writing workers
	SaveMailChan = make(chan *Client, 5)
	// timeout for reads
	if n, n_err = strconv.Atoi(gConfig["GSMTP_TIMEOUT"]); n_err != nil {
		timeout = time.Duration(10)
	} else {
		timeout = time.Duration(n)
	}
	// max email size
	if max_size, n_err = strconv.Atoi(gConfig["GSMTP_MAX_SIZE"]); n_err != nil {
		max_size = 131072
	}

	log.Println("configured")
}

// doWork is the actual main entry function of the application
func doWork() {
	configure()

	//Open database
	dbEmails, _ = db.OpenDB(dataPath)
	randomize.Seed(time.Now().UTC().UnixNano())
	createIfNotIndB("recipients")
	
	//Launch cleaner
	duration, err := strconv.ParseInt(gConfig["CLEANER_INTERVAL"], 10, 64)
	interval := time.NewTicker(time.Second * time.Duration(duration))
	go cleaner(interval)

	pubKeyFile := filepath.Join(exePath, "conf", "public.pem")
	privKeyFile := filepath.Join(exePath, "conf", "private.key")
	cert, err := tls.LoadX509KeyPair(pubKeyFile, privKeyFile)
	if err != nil {
		logln(2, fmt.Sprintf("There was a problem with loading the certificate: %s", err))
	}
	TLSconfig = &tls.Config{Certificates: []tls.Certificate{cert}, ClientAuth: tls.VerifyClientCertIfGiven, ServerName: gConfig["GSMTP_HOST_NAME"]}
	TLSconfig.Rand = rand.Reader
	// start some savemail workers
	for i := 0; i < 3; i++ {
		go saveMail()
	}

	//Start watching modification on db folder / start cleo engine
	BuildIndexes(nil)
	watchFolderRecipients()
	
	//Setup the minimalist webmail interface
	if gConfig["WEBUI_MODE"] != "DISABLED" {
		go setup_webui()
	}
	
	// Start listening for SMTP connections
	listener, err := net.Listen("tcp", gConfig["GSTMP_LISTEN_INTERFACE"])
	if err != nil {
		logln(2, fmt.Sprintf("Cannot listen on port, %v", err))
	} else {
		logln(1, fmt.Sprintf("Listening on tcp %s", gConfig["GSTMP_LISTEN_INTERFACE"]))
	}
	var ClientId int64
	ClientId = 1
	for {
		conn, err := listener.Accept()
		if err != nil {
			logln(1, fmt.Sprintf("Accept error: %s", err))
			continue
		}
		logln(1, fmt.Sprintf(" There are now "+strconv.Itoa(runtime.NumGoroutine())+" serving goroutines"))
		sem <- 1 // Wait for active queue to drain.
		go handleClient(&Client{
			conn:        conn,
			Address:     conn.RemoteAddr().String(),
			time:        time.Now().Unix(),
			bufin:       bufio.NewReader(conn),
			bufout:      bufio.NewWriter(conn),
			ClientId:    ClientId,
			savedNotify: make(chan int),
		})
		ClientId++
	}
}

// stopWork stops the service
func stopWork() {
	logInfo("I'm Stopping!")
}

// logInfo reports a message in the console or the system log,
// depending on the execution context (console or service)
func logInfo(logMessage string, a ...interface{}) {
	if isService {
		logSrv.Info(logMessage, a...)
	} else {
		log.Printf(logMessage, a...)
	}
}

// logInfo reports an error in the console or the system log,
// depending on the execution context (console or service)
func logFatal(logMessage string, a ...interface{}) {
	if isService {
		logSrv.Error(logMessage, a...)
	} else {
		log.Fatalf(logMessage, a...)
	}
}

// checkError checks and reports any fatal error (errors occuring before the HTTP server is listening)
func checkError(err error) {
	if err != nil {
		logFatal("%v", err)
	}
}

func handleClient(client *Client) {
	defer closeClient(client)
	//	defer closeClient(client)
	greeting := "220 " + gConfig["GSMTP_HOST_NAME"] +
		" SMTP Guerrilla-SMTPd #" + strconv.FormatInt(client.ClientId, 10) + " (" + strconv.Itoa(len(sem)) + ") " + time.Now().Format(time.RFC1123Z)
	advertiseTls := "250-STARTTLS\r\n"
	for i := 0; i < 100; i++ {
		switch client.state {
		case 0:
			ResponseAdd(client, greeting)
			client.state = 1
		case 1:
			input, err := readSmtp(client)
			if err != nil {
				logln(1, fmt.Sprintf("Read error: %v", err))
				if err == io.EOF {
					// client closed the connection already
					return
				}
				if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
					// too slow, timeout
					return
				}
				break
			}
			input = strings.Trim(input, " \n\r")
			cmd := strings.ToUpper(input)
			switch {
			case strings.Index(cmd, "HELO") == 0:
				if len(input) > 5 {
					client.Helo = input[5:]
				}
				ResponseAdd(client, "250 "+ gConfig["GSMTP_HOST_NAME"] + " Hello ")
			case strings.Index(cmd, "EHLO") == 0:
				if len(input) > 5 {
					client.Helo = input[5:]
				}
				ResponseAdd(client, "250-"+gConfig["GSMTP_HOST_NAME"]+ " Hello " + client.Helo+"["+client.Address+"]"+"\r\n"+"250-SIZE "+gConfig["GSMTP_MAX_SIZE"]+"\r\n"+advertiseTls+"250 HELP")
			case strings.Index(cmd, "MAIL FROM:") == 0:
				if len(input) > 10 {
					client.Mail_from = input[10:]
				}
				ResponseAdd(client, "250 Ok")
			case strings.Index(cmd, "XCLIENT") == 0:
				// Nginx sends this
				// XCLIENT ADDR=212.96.64.216 NAME=[UNAVAILABLE]
				client.Address = input[13:]
				client.Address = client.Address[0:strings.Index(client.Address, " ")]
				fmt.Println("client Address:[" + client.Address + "]")
				ResponseAdd(client, "250 OK")
			case strings.Index(cmd, "RCPT TO:") == 0:
				if len(input) > 8 {
					client.Rcpt_to = input[8:]
				}
				ResponseAdd(client, "250 Accepted")
			case strings.Index(cmd, "NOOP") == 0:
				ResponseAdd(client, "250 OK")
			case strings.Index(cmd, "RSET") == 0:
				client.Mail_from = ""
				client.Rcpt_to = ""
				ResponseAdd(client, "250 OK")
			case strings.Index(cmd, "DATA") == 0:
				ResponseAdd(client, "354 Enter message, ending with \".\" on a line by itself")
				client.state = 2
			case (strings.Index(cmd, "STARTTLS") == 0) && !client.tls_on:
				ResponseAdd(client, "220 Ready to start TLS")
				// go to start TLS state
				client.state = 3
			case strings.Index(cmd, "QUIT") == 0:
				ResponseAdd(client, "221 Bye")
				killClient(client)
			default:
				ResponseAdd(client, fmt.Sprintf("500 unrecognized command"))
				client.errors++
				if client.errors > 3 {
					ResponseAdd(client, fmt.Sprintf("500 Too many unrecognized commands"))
					killClient(client)
				}
			}
		case 2:
			var err error
			client.Data, err = readSmtp(client)
			if err == nil {
				// to do: timeout when adding to SaveMailChan
				// place on the channel so that one of the save mail workers can pick it up
				SaveMailChan <- client
				// wait for the save to complete
				status := <-client.savedNotify

				if status == 1 {
					ResponseAdd(client, "250 OK : queued as "+client.Hash)
				} else {
					ResponseAdd(client, "554 Error: transaction failed, blame it on the weather")
				}
			} else {
				logln(1, fmt.Sprintf("Data read error: %v", err))
			}
			client.state = 1
		case 3:
			// upgrade to TLS
			var tlsConn *tls.Conn
			tlsConn = tls.Server(client.conn, TLSconfig)
			err := tlsConn.Handshake() // not necessary to call here, but might as well
			if err == nil {
				client.conn = net.Conn(tlsConn)
				client.bufin = bufio.NewReader(client.conn)
				client.bufout = bufio.NewWriter(client.conn)
				client.tls_on = true
			} else {
				logln(1, fmt.Sprintf("Could not TLS handshake:%v", err))
			}
			advertiseTls = ""
			client.state = 1
		}
		// Send a Response back to the client
		err := ResponseWrite(client)
		if err != nil {
			if err == io.EOF {
				// client closed the connection already
				return
			}
			if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
				// too slow, timeout
				return
			}
		}
		if client.kill_time > 1 {
			return
		}
	}

}

func ResponseAdd(client *Client, line string) {
	client.Response = line + "\r\n"
}
func closeClient(client *Client) {
	client.conn.Close()
	<-sem // Done; enable next client to run.
}
func killClient(client *Client) {
	client.kill_time = time.Now().Unix()
}

func readSmtp(client *Client) (input string, err error) {
	var reply string
	// Command state terminator by default
	suffix := "\r\n"
	if client.state == 2 {
		// Data state
		suffix = "\r\n.\r\n"
	}
	for err == nil {
		client.conn.SetDeadline(time.Now().Add(timeout * time.Second))
		reply, err = client.bufin.ReadString('\n')
		if reply != "" {
			input = input + reply
			if len(input) > max_size {
				err = errors.New("Maximum Data size exceeded (" + strconv.Itoa(max_size) + ")")
				return input, err
			}
			if client.state == 2 {
				// Extract the Subject while we are at it.
				scanSubject(client, reply)
			}
		}
		if err != nil {
			break
		}
		if strings.HasSuffix(input, suffix) {
			break
		}
	}
	return input, err
}

// Scan the Data part for a Subject line. Can be a multi-line
func scanSubject(client *Client, reply string) {
	if client.Subject == "" && (len(reply) > 8) {
		test := strings.ToUpper(reply[0:9])
		if i := strings.Index(test, "SUBJECT: "); i == 0 {
			// first line with \r\n
			client.Subject = reply[9:]
		}
	} else if strings.HasSuffix(client.Subject, "\r\n") {
		// chop off the \r\n
		client.Subject = client.Subject[0 : len(client.Subject)-2]
		if (strings.HasPrefix(reply, " ")) || (strings.HasPrefix(reply, "\t")) {
			// Subject is multi-line
			client.Subject = client.Subject + reply[1:]
		}
	}
}

func ResponseWrite(client *Client) (err error) {
	var size int
	client.conn.SetDeadline(time.Now().Add(timeout * time.Second))
	size, err = client.bufout.WriteString(client.Response)
	client.bufout.Flush()
	client.Response = client.Response[size:]
	return err
}

// saveMail receives values from the channel repeatedly until it is closed.
func saveMail() {

	for {
		client := <-SaveMailChan
		client.Subject = mimeHeaderDecode(client.Subject)
		client.Hash = md5hex(client.Rcpt_to + client.Mail_from + client.Subject + strconv.FormatInt(time.Now().UnixNano(), 10))
		to := strings.Replace(client.Rcpt_to, "<", "", -1)
		to = strings.Replace(to, ">", "", -1)
		from := strings.Replace(client.Mail_from, "<", "", -1)
		from = strings.Replace(from, ">", "", -1)
		timestamp := time.Now().Format("20060102150405.000000000")
		
		createIfNotIndB(to)
		emails := dbEmails.Use(to)
		_, err := emails.Insert(map[string]interface{}{
			"timestamp": timestamp,
			"from":  from,
			"subject":  client.Subject,
			"data":  client.Data,
			"address":  client.Address})
		if err != nil {
			panic(err)
		}
		//fmt.Println("++++", id, client.Rcpt_to, client.Subject)
		client.savedNotify <- 1
	}
}

// Decode strings in Mime header format
// eg. =?ISO-2022-JP?B?GyRCIVo9dztSOWJAOCVBJWMbKEI=?=
func mimeHeaderDecode(str string) string {
	reg, _ := regexp.Compile(`=\?(.+?)\?([QBqp])\?(.+?)\?=`)
	matched := reg.FindAllStringSubmatch(str, -1)
	var charset, encoding, payload string
	if matched != nil {
		for i := 0; i < len(matched); i++ {
			if len(matched[i]) > 2 {
				charset = matched[i][1]
				encoding = strings.ToUpper(matched[i][2])
				payload = matched[i][3]
				switch encoding {
				case "B":
					str = strings.Replace(str, matched[i][0], mailTransportDecode(payload, "base64", charset), 1)
				case "Q":
					str = strings.Replace(str, matched[i][0], mailTransportDecode(payload, "quoted-printable", charset), 1)
				}
			}
		}
	}
	return str
}

// decode from 7bit to 8bit UTF-8
// encoding_type can be "base64" or "quoted-printable"
func mailTransportDecode(str string, encoding_type string, charset string) string {
	if charset == "" {
		charset = "UTF-8"
	} else {
		charset = strings.ToUpper(charset)
	}
	if encoding_type == "base64" {
		str = fromBase64(str)
	} else if encoding_type == "quoted-printable" {
		str = fromQuotedP(str)
	}
	if charset != "UTF-8" {
		charset = fixCharset(charset)
		// eg. charset can be "ISO-2022-JP"

		//TODO
		//convstr, err := iconv.Conv(str, "UTF-8", charset)

		//sr := strings.NewReader(str)
		//tr := transform.NewReader(sr, charmap.Windows1252.NewDecoder())

		//CodePage437
		//CodePage866
		//ISO8859_2

		/*if err == nil {
			return convstr
		}*/
	}
	return str
}

func fromBase64(Data string) string {
	buf := bytes.NewBufferString(Data)
	decoder := base64.NewDecoder(base64.StdEncoding, buf)
	res, _ := ioutil.ReadAll(decoder)
	return string(res)
}

func fromQuotedP(Data string) string {
	buf := bytes.NewBufferString(Data)
	decoder := qprintable.NewDecoder(qprintable.BinaryEncoding, buf)
	res, _ := ioutil.ReadAll(decoder)
	return string(res)
}

func fixCharset(charset string) string {
	reg, _ := regexp.Compile(`[_:.\/\\]`)
	fixed_charset := reg.ReplaceAllString(charset, "-")
	// Fix charset
	// borrowed from http://squirrelmail.svn.sourceforge.net/viewvc/squirrelmail/trunk/squirrelmail/include/languages.php?revision=13765&view=markup
	// OE ks_c_5601_1987 > cp949
	fixed_charset = strings.Replace(fixed_charset, "ks-c-5601-1987", "cp949", -1)
	// Moz x-euc-tw > euc-tw
	fixed_charset = strings.Replace(fixed_charset, "x-euc", "euc", -1)
	// Moz x-windows-949 > cp949
	fixed_charset = strings.Replace(fixed_charset, "x-windows_", "cp", -1)
	// windows-125x and cp125x charsets
	fixed_charset = strings.Replace(fixed_charset, "windows-", "cp", -1)
	// ibm > cp
	fixed_charset = strings.Replace(fixed_charset, "ibm", "cp", -1)
	// iso-8859-8-i -> iso-8859-8
	fixed_charset = strings.Replace(fixed_charset, "iso-8859-8-i", "iso-8859-8", -1)
	if charset != fixed_charset {
		return fixed_charset
	}
	return charset
}

func md5hex(str string) string {
	h := md5.New()
	h.Write([]byte(str))
	sum := h.Sum([]byte{})
	return hex.EncodeToString(sum)
}
