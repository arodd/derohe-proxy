package main

import "time"

var command_line string = `derohe-proxy
Proxy to combine all miners and to reduce network load

Usage:
  derohe-proxy [--listen-address=<127.0.0.1:10100>] [--log-interval=<60>] [--jobrate=<500ms>] [--minimal] [--nonce] [--zero] [--global] [--verbose] --daemon-address=<1.2.3.4:10100> [--testnet]

Options:
 --listen-address=<127.0.0.1:10100>		bind to specific address:port, default is 0.0.0.0:10200
 --daemon-address=<1.2.3.4:10100>		connect to this daemon
 --log-interval=<60>   set logging interval in seconds (range 60 - 3600), default is 60 seconds
 --jobrate=<500ms>	time between dispatch jobs - will always update on height/difficulty change, default is 500ms
 --minimal   forward only 2 jobs per block (1 for miniblocks and 1 for final miniblock), by default all jobs are forwarded
 --nonce   enable nonce/flag editing, default is off
 --zero   enable zero nonce2/flags
 --global   enable global nonce sharing for first 9 bytes, only enabled with nonce editing flag 
 --verbose   enable nonce/flag printing, default is off

Example Mainnet: ./derohe-proxy --daemon-address=minernode1.dero.io:10100
`

// program arguments
var Arguments = map[string]interface{}{}

var listen_addr string = "0.0.0.0:10200"
var daemon_address string = "minernode1.dero.io:10100"

// logging interval in seconds
var log_intervall int = 60

// send only 2 jobs per block
var minimal bool = false

// edit nonce
var nonce bool = false

// edit nonce globally with small unique space for clients
var global bool = false

// print nonces/flags
var verbose bool = false

// zero nonce2/flags
var zero bool = false

// job dispatch rate
var jobrate time.Duration = (500 * time.Millisecond)
