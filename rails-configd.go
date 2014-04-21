// Copyright 2014 Ruben Fonseca
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.
//
// Rails-configd - an online rails configuration generator using etcd data
//
// Standard usage:
//   (inside your Rails app)
//   $ rails-configd -etcd http://localhost:4001 -etcd-dir /rails/production -env production -renderer yaml -reloader touch
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/coreos/go-etcd/etcd"
	"github.com/rubenfonseca/rails-configd/src"
)

var usageMessage = `This is a tool for watching over etcd tree, create config files for Rails, and restart the Rails processes.

Usage: %s [options]

The following options are recognized:
`

func usage() {
	fmt.Fprintf(os.Stderr, usageMessage, os.Args[0])
	flag.PrintDefaults()
	os.Exit(2)
}

func loop(receiverChannel chan *etcd.Response, env src.Env) {
	for response := range receiverChannel {
		key := env.NakedKey(response.Node.Key, *env.EtcdDir)
		parts := strings.Split(key, "/")
		env.UpdateData(parts, response.Node.Value, response.Action, env.Data)

		log.Printf("[CHANGE]: %s %s %s", response.Action, key, response.Node.Value)

		env.Cycle()
	}
}

func main() {
	env := src.Env{}
	env.Data = make(map[string]interface{})

	env.Etcd = flag.String("etcd", "http://localhost:4001", "etcd address location")
	env.EtcdDir = flag.String("etcd-dir", "/rails/production", "etcd directory that contains the configurations")

	env.RailsEnv = flag.String("env", "production", "The Rails environment to configure")
	rendererPtr := flag.String("renderer", "yaml", "The renderer to use when outputing the configs")
	reloaderPtr := flag.String("reloader", "touch", "The strategy to reload the Rails app")

	src.RegisterRendererFlags()
	src.RegisterReloaderFlags()

	flag.Usage = usage
	flag.Parse()

	// renderer
	renderer, err := src.OpenRenderer(*rendererPtr)
	if err != nil {
		panic(err)
	}
	env.Renderer = renderer

	// reloader
	env.Reloader, err = src.OpenReloader(*reloaderPtr)
	if err != nil {
		panic(err)
	}

	// etcd
	receiverChannel := make(chan *etcd.Response)
	stopChannel := make(chan bool)
	etcdClient := etcd.NewClient([]string{*env.Etcd})
	success := etcdClient.SyncCluster()
	if !success {
		log.Fatal("Cannot sync with etcd machines, please check --etcd")
	}

	etcdResponse, err := etcdClient.Get(*env.EtcdDir, false, true)
	if err != nil {
		panic(err)
	}
	if !etcdResponse.Node.Dir {
		panic("etc-dir should be a directory")
	}
	env.BuildData(*etcdResponse.Node, *env.EtcdDir, env.Data)
	env.Cycle()

	log.Printf("[MAIN] Waiting for changes from etcd @ %s", *env.EtcdDir)
	go etcdClient.Watch(*env.EtcdDir, 0, true, receiverChannel, stopChannel)

	// signals
	osSignal := make(chan os.Signal)
	signal.Notify(osSignal, os.Interrupt)
	go func() {
		for _ = range osSignal {
			log.Print("Interrupt received, finishing")
			stopChannel <- true
		}
	}()

	loop(receiverChannel, env)
}