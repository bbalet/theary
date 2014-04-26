# What is Theary ?

Theary is a fake SMTP server that temporarily stores received e-mails. It offers a minimalist webmail to display received e-mails. Theary was designed to offer a volatile SMTP server for demo or load test purposes. It was inspired by smtp4dev, but with web and volume in mind.

Received e-mails are deleted on a reguler basis (depending on ```RECIPIENTS_LIFETIME``` value).

# Features not covered

* Theary doesn't relay e-mails to any hytpothetical final recipient.
* Theary uses a nosql db to temporarily store received e-mails, but is not designed to permantly store e-mails.
* Theary implements no security features (welcome to pirates).

# Usage

* ```theary run``` run the executable from command line
* ```theary install``` install the service
* ```theary install``` remove the service
* ```theary start``` start the service
* ```theary stop``` stop the service

# Configuration

Edit conf/conf.json :

* ```GM_ALLOWED_HOSTS``` Allowed hosts (comma separated) or any host (```*```).
* ```GSMTP_HOST_NAME``` Fake SMTP hostname
* ```GSMTP_MAX_SIZE``` Max size of e-mails
* ```GSMTP_TIMEOUT``` SMTP timeout
* ```GSMTP_VERBOSE``` Level of log
* ```GSTMP_LISTEN_INTERFACE``` host:port to be listened by the SMTP listener.
* ```GM_MAX_CLIENTS``` Maximun of clients served by the SMTP listener.
* ```WEBUI_MODE``` Mode of the web user interface (embedded web server or served by fastCGI) :
** LOCAL Run as a local web server.
** TCP Serve as FCGI via TCP.
** UNIX Run as FCGI via UNIX socket.
* ```WEBUI_SERVE``` host:port to be listened by the web user interface (minimalist webmail client).
* ```RECIPIENTS_LIFETIME``` lifetime (in seconds) of a recipient. If older, will be deleted by the cleaner.
* ```CLEANER_INTERVAL``` duration (in seconds) between two calls to the function that cleans the database.

# Status

Theary is under development.

# Licence

Release under GPL v3

# Supported environnements

Theary is written in pure go and doesn't depend on 3rd party C bindings, so it can run on any environnement supported by go (Windows, Linux, etc.)

# Credits

Theary is derivated from https://github.com/shirkey/go-guerrilla project, but has a different purpose. Instead of persisting received mail into a MySQL database, it temporarily strores them in a nosql db in order to display them on a lightweight embedded web ui. Theary doesn't interface with Nginx through a proxy interface but with any webserver supporting FastCGI.

The typeahead algo is derived from https://github.com/jamra/gocleo but with an optimized code.

Icon by Maja Bencic - http://www.fritula.hr under Creative Commons (Attribution 3.0 Croatia)

Theary would not exist without these open source libraries :
* github.com/HouzuoGuo/tiedot
* bitbucket.org/kardianos/osext
* bitbucket.org/kardianos/service
* github.com/gorilla
* golang amazing std lib
