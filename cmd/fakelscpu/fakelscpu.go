package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
)

const (
	fakeCoreTopology = "/testdata/fakelscpu.core"
	fakeNodeTopology = "/testdata/fakelscpu.node"
)

func main() {
	var (
		mode string
		file []byte
		err  error
	)
	flag.StringVar(&mode, "p", "", "")
	flag.Parse()
	topologyFilesDir := os.Getenv("POOLER_TEST_DIR")
	if strings.Contains(mode, "core") {
		file, err = ioutil.ReadFile(topologyFilesDir + fakeCoreTopology)
	} else {
		file, err = ioutil.ReadFile(topologyFilesDir + fakeNodeTopology)
	}
	if err != nil {
		log.Println("Error opening file:" + err.Error())
		return
	}
	fmt.Println(string(file))
}
