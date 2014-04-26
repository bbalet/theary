package main

/*
 * This file is part of theary.
 * 
 * It uses portion of code from gocleo
 * Copyright (c) 2011 jamra.source@gmail.com
 * Licensed under the Apache License, Version 2.0 (the "License"); 
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
	"fmt"
	"net/http"
	"sort"
	"io/ioutil"
	"strings"
	"encoding/json"
	"code.google.com/p/go.exp/fsnotify"
	"github.com/gorilla/mux"
)

var watcher fsnotify.Watcher

// watchFolderRecipients watch if there is any modification in the list of recipient and update
// the index of suggestions (for autocompletion purposes in the web ui).
func watchFolderRecipients() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logFatal("(fsConfigWatcher) fsnotify.NewWatcher() : ", err)
	}

	go func() {
		for {
			select {
			case ev := <-watcher.Event:
				logInfo("event: %v", ev)
				BuildIndexes(nil)
			case err := <-watcher.Error:
				logInfo("error: %v", err)
			}
		}
	}()

	err = watcher.WatchFlags(dataPath, fsnotify.FSN_MODIFY | fsnotify.FSN_CREATE | fsnotify.FSN_DELETE)
	if err != nil {
		logln(2, fmt.Sprintf("(fsConfigWatcher) watcher.WatchFlags(fsnotify.FSN_MODIFY) :  %s", err))
	}
	
}

//Search handles the web requests and writes the output as
//json data.
func searchHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	vars := mux.Vars(r)
	query := vars["query"]
	searchResult := CleoSearch(m.iIndex, m.fIndex, query)
	sort.Sort(ByScore{searchResult})
	myJson, _ := json.Marshal(searchResult)
	fmt.Fprintf(w, string(myJson))
}

func BuildIndexes(scoringFunction fn_score) {
	m = &indexContainer{}
	m.iIndex = NewInvertedIndex()
	m.fIndex = NewForwardIndex()

	chosenScoringFunction = scoringFunction
	if scoringFunction == nil {
		chosenScoringFunction = Score
	}

	//Read folder content
	docID := 1
	files, _ := ioutil.ReadDir(dataPath)
    for _, f := range files {
		if f.IsDir() && f.Name() != "recipients" {
			filter := computeBloomFilter(f.Name())
			m.iIndex.AddDoc(docID, f.Name(), filter) //insert into inverted index
			m.fIndex.AddDoc(docID, f.Name())         //Insert into forward index
			docID++
		}
	}
}

func LevenshteinDistance(a, b *string) int {
	la := len(*a)
	lb := len(*b)
	d  := make([]int, la + 1)
	var lastdiag, olddiag, temp int
	
	for i := 1; i <= la; i++ {
		d[i] = i
	}
	for i := 1; i <= lb; i++ {
		d[0] = i
		lastdiag = i - 1
		for j := 1; j <= la; j++ {
			olddiag = d[j]
			min := d[j] + 1
			if (d[j - 1] + 1) < min {
				min = d[j - 1] + 1
			}
			if ( (*a)[j - 1] == (*b)[i - 1] ) {
				temp = 0
			} else {
				temp = 1
			}
			if (lastdiag + temp) < min {
				min = lastdiag + temp
			}
			d[j] = min
			lastdiag = olddiag
		}
	}
	return d[la]
}

func Min(a ...int) int {
	min := int(^uint(0) >> 1) // largest int
	for _, i := range a {
		if i < min {
			min = i
		}
	}
	return min
}
func Max(a ...int) int {
	max := int(0)
	for _, i := range a {
		if i > max {
			max = i
		}
	}
	return max
}

type indexContainer struct {
	iIndex *InvertedIndex
	fIndex *ForwardIndex
}

var m *indexContainer
var chosenScoringFunction fn_score
	
type RankedResults []RankedResult
type ByScore struct{ RankedResults }

func (s RankedResults) Len() int      { return len(s) }
func (s RankedResults) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s ByScore) Less(i, j int) bool  { return s.RankedResults[i].Score > s.RankedResults[j].Score }

type RankedResult struct {
	Word  string
	Score float64
}

//This is the meat of the search.  It first checks the inverted index
//for matches, then filters the potentially numerous results using
//the bloom filter.  Finally, it ranks the word using a Levenshtein
//distance.
func CleoSearch(iIndex *InvertedIndex, fIndex *ForwardIndex, query string) []RankedResult {
	rslt := make([]RankedResult, 0, 0)

	candidates := iIndex.Search(query) //First get candidates from Inverted Index
	qBloom := computeBloomFilter(query)

	for _, i := range candidates {
		if TestBytesFromQuery(i.bloom, qBloom) == true { //Filter using Bloom Filter
			c := fIndex.itemAt(i.docId)              //Get whole document from Forward Index
			score := chosenScoringFunction(query, c) //Score the Forward Index between 0-1
			ranked := RankedResult{c, score}
			rslt = append(rslt, ranked)
		}
	}
	return rslt
}

//Iterates through all of the 8 bytes (64 bits) and tests
//each bit that is set to 1 in the query's filter against
//the bit in the comparison's filter.  If the bit is not
// also 1, you do not have a match.
func TestBytesFromQuery(bf int, qBloom int) bool {
	for i := uint(0); i < 64; i++ {
		//a & (1 << idx) == b & (1 << idx)
		if (bf&(1<<i) != (1 << i)) && qBloom&(1<<i) == (1<<i) {
			return false
		}
	}
	return true
}

func Score(query, candidate string) float64 {
	lev := LevenshteinDistance(&query, &candidate)
	length := Max(len(candidate), len(query))
	return float64(length-lev) / float64(length+lev) //Jacard score
}

func getPrefix(query string) string {
	qLen := Min(len(query), 4)
	q := query[0:qLen]
	return strings.ToLower(q)
}

type Document struct {
	docId int
	bloom int
}

//Used for the bloom filter
const (
	FNV_BASIS_64 = uint64(14695981039346656037)
	FNV_PRIME_64 = uint64((1 << 40) + 435)
	FNV_MASK_64  = uint64(^uint64(0) >> 1)
	NUM_BITS     = 64

	FNV_BASIS_32 = uint32(0x811c9dc5)
	FNV_PRIME_32 = uint32((1 << 24) + 403)
	FNV_MASK_32  = uint32(^uint32(0) >> 1)
)

type fn_score func(word, query string) (score float64)

//The bloom filter of a word is 8 bytes in length
//and has each character added separately
func computeBloomFilter(s string) int {
	cnt := len(s)

	if cnt <= 0 {
		return 0
	}

	var filter int
	hash := uint64(0)

	for i := 0; i < cnt; i++ {
		c := s[i]

		//first hash function
		hash ^= uint64(0xFF & c)
		hash *= FNV_PRIME_64

		//second hash function (reduces collisions for bloom)
		hash ^= uint64(0xFF & (c >> 16))
		hash *= FNV_PRIME_64

		//position of the bit mod the number of bits (8 bytes = 64 bits)
		bitpos := hash % NUM_BITS
		if bitpos < 0 {
			bitpos += NUM_BITS
		}
		filter = filter | (1 << bitpos)
	}

	return filter
}

//Inverted Index - Maps the query prefix to the matching documents
type InvertedIndex map[string][]Document

func NewInvertedIndex() *InvertedIndex {
	i := make(InvertedIndex)
	return &i
}

func (x *InvertedIndex) Size() int {
	return len(map[string][]Document(*x))
}

func (x *InvertedIndex) AddDoc(docId int, doc string, bloom int) {
	for _, word := range strings.Fields(doc) {
		word = getPrefix(word)

		ref, ok := (*x)[word]
		if !ok {
			ref = nil
		}

		(*x)[word] = append(ref, Document{docId: docId, bloom: bloom})
	}
}

func (x *InvertedIndex) Search(query string) []Document {
	q := getPrefix(query)

	ref, ok := (*x)[q]

	if ok {
		return ref
	}
	return nil
}

//Forward Index - Maps the document id to the document
type ForwardIndex map[int]string

func NewForwardIndex() *ForwardIndex {
	i := make(ForwardIndex)
	return &i
}
func (x *ForwardIndex) AddDoc(docId int, doc string) {
	for _, word := range strings.Fields(doc) {
		_, ok := (*x)[docId]
		if !ok {
			(*x)[docId] = word
		}
	}
}
func (x *ForwardIndex) itemAt(i int) string {
	return (*x)[i]
}


