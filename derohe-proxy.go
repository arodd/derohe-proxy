package main

import (
	"derohe-proxy/config"
	"derohe-proxy/proxy"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/docopt/docopt-go"
)

var proxyConfig = config.ProxyConfig{
	ListenAddr:  "0.0.0.0:10200",
	DaemonAddr:  "minernode1.dero.io:10100",
	LogInterval: 60,
	Minimal:     false,
	NonceEdit:   false,
	Global:      false,
	ZeroNonce:   false,
	Verbose:     false,
	JobRate:     (500 * time.Millisecond),
	WalletFile:  false,
	Quantum:     false,
}

func main() {
	var err error
	config.Arguments, err = docopt.Parse(config.CommandLine, nil, true, "pre-alpha", false)

	if err != nil {
		return
	}

	if config.Arguments["--listen-address"] != nil {
		addr, err := net.ResolveTCPAddr("tcp", config.Arguments["--listen-address"].(string))
		if err != nil {
			return
		} else {
			if addr.Port == 0 {
				return
			} else {
				proxyConfig.ListenAddr = addr.String()
			}
		}
	}

	if config.Arguments["--daemon-address"] == nil {
		return
	} else {
		proxyConfig.DaemonAddr = config.Arguments["--daemon-address"].(string)
	}

	if config.Arguments["--log-interval"] != nil {
		interval, err := strconv.ParseInt(config.Arguments["--log-interval"].(string), 10, 32)
		if err != nil {
			return
		} else {
			if interval < 60 || interval > 3600 {
				proxyConfig.LogInterval = 60
			} else {
				proxyConfig.LogInterval = int(interval)
			}
		}
	}
	if config.Arguments["--jobrate"] != nil {
		job_rate, err := time.ParseDuration(config.Arguments["--jobrate"].(string))
		if err != nil {
			return
		} else {
			proxyConfig.JobRate = job_rate
		}
	}

	if config.Arguments["--minimal"].(bool) {
		proxyConfig.Minimal = true
		fmt.Printf("%v Forward only 2 jobs per block\n", time.Now().Format(time.Stamp))
	}

	if config.Arguments["--nonce"].(bool) {
		proxyConfig.NonceEdit = true
		fmt.Printf("%v Nonce editing is enabled\n", time.Now().Format(time.Stamp))
	}

	if config.Arguments["--zero"].(bool) {
		proxyConfig.ZeroNonce = true
		fmt.Printf("%v Flags/Nonce2 zeroing are enabled\n", time.Now().Format(time.Stamp))
	}

	if config.Arguments["--global"].(bool) {
		proxyConfig.Global = true
		fmt.Printf("%v Global Nonce targeting is enabled\n", time.Now().Format(time.Stamp))
	}

	if config.Arguments["--walletfile"].(bool) {
		proxyConfig.WalletFile = true
		fmt.Printf("%v Global Wallet List Enabled\n", time.Now().Format(time.Stamp))
	}

	if config.Arguments["--quantum"].(bool) {
		proxyConfig.Quantum = true
		fmt.Printf("%v Quantum Random Data Enabled...will fallback to crypto/rand if unavailable\n", time.Now().Format(time.Stamp))
	}

	if config.Arguments["--verbose"].(bool) {
		proxyConfig.Verbose = true
		fmt.Printf("%v Verbose nonce output is enabled\n", time.Now().Format(time.Stamp))
	}

	fmt.Printf("%v Logging every %d seconds\n", time.Now().Format(time.Stamp), proxyConfig.LogInterval)
	fmt.Printf("%v Job Dispatch Rate: %s\n", time.Now().Format(time.Stamp), proxyConfig.JobRate.String())
	if proxyConfig.NonceEdit {
		go proxy.RandomGenerator(&proxyConfig)

		fmt.Print("Building random data for 10 sec...\n")
		time.Sleep(time.Second * 10)
	}
	go proxy.Start_server(&proxyConfig)

	// Wait for first miner connection to grab wallet address
	for proxy.CountMiners() < 1 {
		time.Sleep(time.Second * 1)
	}

	go proxy.Start_client(proxy.Address)
	go proxy.SendUpdateToDaemon()

	for {
		time.Sleep(time.Second * time.Duration(proxyConfig.LogInterval))
		fmt.Printf("%v %d miners connected, Bl: %d, Mbl: %d, Rej: %d\n", time.Now().Format(time.Stamp), proxy.CountMiners(), proxy.Blocks, proxy.Minis, proxy.Rejected)
		for i := range proxy.Wallet_count {
			if proxy.Wallet_count[i] > 1 {
				fmt.Printf("%v Wallet %v, %d miners\n", time.Now().Format(time.Stamp), i, proxy.Wallet_count[i])
			}
		}
		if proxyConfig.WalletFile {
			proxy.LoadWalletsFile()
		}
	}
}
