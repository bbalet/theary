package main

/*
 * This file is part of theary.
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
	"time"
	"io/ioutil"
	"strconv"
	"log"
)

// createIfNotIndB checks if a collection exists or not
// it will create the collection if it doen't exist
func createIfNotIndB(collectionName string ) {
	if !existsIndB(collectionName) {
		if err := dbEmails.Create(collectionName, 1); err != nil {
			panic(err)
		}
	}
}

// existsIndB checks if a collection exists or not
func existsIndB(collectionName string ) bool {
	found := false
	for name := range dbEmails.StrCol {
		if name == collectionName {
			found = true
			break
		}
	}
	return found
}

// cleaner is regularly triggered to delete old mails and recipients from database
func cleaner(interval *time.Ticker) {
	var refTime time.Time
	for _ = range interval.C {
		duration, _ := strconv.ParseInt(gConfig["RECIPIENTS_LIFETIME"], 10, 64)
		duration = duration * -1
		refTime = time.Now().Add(time.Duration(duration) * time.Second)
		files, _ := ioutil.ReadDir(dataPath)
		for _, f := range files {
			if f.IsDir() && f.Name() != "recipients" {
				if f.ModTime().Before(refTime) {
					dbEmails.Drop(f.Name())
					log.Println("Recipient dropped as it exceeded its lifetime", f.Name())
				}
			}
		}
	}
}
