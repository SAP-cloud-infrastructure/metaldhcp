// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"

	"github.com/pin/tftp/v3"
	"github.com/sirupsen/logrus"
)

var log = logrus.New()

func main() {
	var root, addr string
	flag.StringVar(&root, "root", "/tftp-root", "directory to serve files from")
	flag.StringVar(&addr, "addr", ":69", "UDP address to listen on")
	flag.Parse()

	s := tftp.NewServer(readHandler(root), nil)
	log.Infof("TFTP server listening on %s serving %s", addr, root)
	if err := s.ListenAndServe(addr); err != nil {
		log.Fatalf("TFTP server: %v", err)
	}
}

func readHandler(root string) func(string, io.ReaderFrom) error {
	return func(filename string, rf io.ReaderFrom) error {
		path := filepath.Join(root, filepath.Base(filename))
		f, err := os.Open(path)
		if err != nil {
			log.Warnf("tftp: %s: %v", filename, err)
			return err
		}
		defer f.Close()
		n, err := rf.ReadFrom(f)
		if err != nil {
			log.Warnf("tftp: sending %s: %v", filename, err)
			return err
		}
		remote := rf.(tftp.OutgoingTransfer).RemoteAddr()
		log.Infof("tftp: sent %s (%d bytes) to %s", filename, n, remote.String())
		return nil
	}
}
