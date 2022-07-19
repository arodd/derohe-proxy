package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"derohe-proxy/config"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arodd/qrand"

	"github.com/lesismal/nbio/nbhttp/websocket"

	"github.com/deroproject/derohe/globals"
	"github.com/deroproject/derohe/rpc"
	"github.com/deroproject/graviton"
	"github.com/lesismal/llib/std/crypto/tls"
	"github.com/lesismal/nbio"
	"github.com/lesismal/nbio/nbhttp"
)

var proxyConfig *config.ProxyConfig
var server *nbhttp.Server

var memPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 16*1024)
	},
}

type user_session struct {
	blocks        uint64
	miniblocks    uint64
	lasterr       string
	address       rpc.Address
	orphans       uint64
	hashrate      float64
	valid_address bool
	address_sum   [32]byte
	threads       int
}
type ( // array without name containing block template in hex
	MinerInfo_Params struct {
		Wallet_Address string  `json:"wallet_address"`
		Miner_Tag      string  `json:"miner_tag"`
		Miner_Hashrate float64 `json:"miner_hashrate"`
	}
	MinerInfo_Result struct {
	}
)
type work_template struct {
	NonceData   [3]uint32
	Flags       uint32
	SharedNonce uint32
}

type wallets struct {
	Wallet    []string
	WalletSum [][32]byte
}

var client_list_mutex sync.RWMutex
var client_list = map[*websocket.Conn]*user_session{}

var miners_count int
var Wallet_count map[string]uint
var Address string

var wallet_list = wallets{}
var walletIndex int = 0

//var blocksFound int = 0
//var timeIncrement uint16 = 1

func Start_server(configData *config.ProxyConfig) {
	var err error
	proxyConfig = configData
	if proxyConfig.WalletFile {
		LoadWalletsFile()
	}
	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{generate_random_tls_cert()},
		InsecureSkipVerify: true,
	}

	mux := &http.ServeMux{}
	mux.HandleFunc("/", onWebsocket) // handle everything

	server = nbhttp.NewServer(nbhttp.Config{
		Name:                    "GETWORK",
		Network:                 "tcp",
		AddrsTLS:                []string{proxyConfig.ListenAddr},
		TLSConfig:               tlsConfig,
		Handler:                 mux,
		MaxLoad:                 10 * 1024,
		MaxWriteBufferSize:      32 * 1024,
		ReleaseWebsocketPayload: true,
		KeepaliveTime:           240 * time.Hour, // we expects all miners to find a block every 10 days,
		NPoller:                 runtime.NumCPU(),
	})

	server.OnReadBufferAlloc(func(c *nbio.Conn) []byte {
		return memPool.Get().([]byte)
	})
	server.OnReadBufferFree(func(c *nbio.Conn, b []byte) {
		memPool.Put(b)
	})

	if err = server.Start(); err != nil {
		return
	}

	Wallet_count = make(map[string]uint)

	server.Wait()
	defer server.Stop()

}

func CountMiners() int {
	client_list_mutex.RLock()
	defer client_list_mutex.RUnlock()
	miners_count = len(client_list)
	return miners_count
}

var random_data_lock sync.RWMutex

var MyRandomData []byte
var fillRandom bool = true

func GetRandomByte(bytes int) ([]byte, error) {
	var err error

	picked_data := make([]byte, bytes)
	if len(MyRandomData) > bytes {
		random_data_lock.Lock()
		picked_data = MyRandomData[len(MyRandomData)-bytes:]
		MyRandomData = MyRandomData[:len(MyRandomData)-bytes]
		random_data_lock.Unlock()
	} else {
		err = errors.New("Error Retrieving Random Bytes from Cache")
	}
	return picked_data, err
}

func RandomGenerator(config *config.ProxyConfig) {

	chunk_size := 819200

	fmt.Print("Generating random data...\n")
	for {
		random_data_lock.Lock()

		if len(MyRandomData) < chunk_size || fillRandom {

			fillRandom = true
			newdata := make([]byte, 1024)
			// var byte_size int
			var err error
			// start := time.Now()
			if config.Quantum {
				_, err = qrand.Read(newdata[:])
				if err != nil {
					fmt.Printf("Error when fetching quantum random data, falling back to crypto rand: %s\n", err.Error())
					_, err = rand.Read(newdata[:])
				}
			} else {
				_, err = rand.Read(newdata[:])
			}
			if err == nil {
				for x, _ := range newdata {
					MyRandomData = append(MyRandomData, newdata[x])
				}
				// fmt.Printf("Generated %d bytes of random data in %s (Data store size: %d)\n", byte_size, time.Since(start).String(), len(MyRandomData))
				if len(MyRandomData) >= chunk_size*10 {
					fillRandom = false
				}
			} else {
				fmt.Printf("Error when fetching random data: %s\n", err.Error())
				time.Sleep(time.Second * 5)
			}

			random_data_lock.Unlock()
		} else {
			random_data_lock.Unlock()

			// fmt.Print("Sleeping\n")
			time.Sleep(time.Second * 5)
		}

	}
}

func GetGlobalWork() work_template {
	var work_data = work_template{}

	if proxyConfig.Global && proxyConfig.NonceEdit && proxyConfig.ZeroNonce {
		work_data.NonceData[0] = RandomUint32()
	} else if proxyConfig.Global && proxyConfig.NonceEdit {
		work_data.NonceData[0] = RandomUint32()
		work_data.SharedNonce = RandomUint32()
	}
	return work_data
}

func GetClientWork(work_data work_template, total_threads uint32) work_template {
	if proxyConfig.NonceEdit && proxyConfig.Global {
		work_data.SharedNonce += (256 * total_threads)
		work_data.NonceData[1] = work_data.SharedNonce
		work_data.NonceData[2] = RandomUint32()
	} else if proxyConfig.NonceEdit && proxyConfig.ZeroNonce {
		_, err := GetRandomByte(1)
		if err != nil {
			fmt.Println(err)
		}
		work_data.NonceData[0] = RandomUint32()
		work_data.NonceData[2] = RandomUint32()
	} else if proxyConfig.NonceEdit {
		_, err := GetRandomByte(1)
		if err != nil {
			fmt.Println(err)
		}
		work_data.NonceData[0] = RandomUint32()
		work_data.NonceData[1] = RandomUint32()
		work_data.NonceData[2] = RandomUint32()
		work_data.Flags = RandomUint32()
	}
	return work_data
}

func SendTemplateToNode(data []byte, work_data work_template, total_threads uint32, ck *websocket.Conn, wallet [32]byte) {
	if result := edit_blob(data, wallet, GetClientWork(work_data, total_threads)); result != nil {
		data = result
	} else {
		fmt.Println(time.Now().Format(time.Stamp), "Failed to change nonce / miner keyhash")
	}
	client_list_mutex.Lock()
	defer globals.Recover(2)
	ck.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
	ck.WriteMessage(websocket.TextMessage, data)
	client_list_mutex.Unlock()
}

// forward all incoming templates from daemon to all miners
func SendTemplateToNodes(data []byte) {
	var total_threads uint32 = 0
	var wallet [32]byte
	work_data := GetGlobalWork()
	client_list_mutex.RLock()
	defer client_list_mutex.RUnlock()
	if walletIndex < (len(wallet_list.Wallet)-1) && proxyConfig.WalletFile {
		walletIndex++
	} else {
		walletIndex = 0
	}
	for rk, rv := range client_list {
		if client_list == nil {
			break
		}
		if proxyConfig.WalletFile {
			wallet = wallet_list.WalletSum[walletIndex]
		} else {
			wallet = rv.address_sum
		}
		go SendTemplateToNode(data, work_data, total_threads, rk, wallet)
		total_threads += uint32(rv.threads)
	}
}

func LoadWalletsFile() {
	if _, err := os.Stat("wallets.json"); errors.Is(err, os.ErrNotExist) {
		os.Exit(1)
	}
	file, err := os.Open("wallets.json")
	if err != nil {
	} else {
		defer file.Close()
		decoder := json.NewDecoder(file)
		err = decoder.Decode(&wallet_list)
		if err != nil {
			os.Exit(1)
		} else { // successfully unmarshalled data
			walletsums := make([][32]byte, len(wallet_list.Wallet))
			for i, wallet := range wallet_list.Wallet {
				addr, _ := globals.ParseValidateAddress(wallet)
				addr_raw := addr.PublicKey.EncodeCompressed()
				walletsums[i] = graviton.Sum(addr_raw)
			}
			wallet_list.WalletSum = walletsums
		}
	}
}

func RandomUint16() uint16 {
	var chunk uint16

	chunk_size := 2
	random_blob, _ := GetRandomByte(chunk_size)
	chunk = binary.BigEndian.Uint16(random_blob)
	return chunk
}

func RandomUint32() uint32 {
	var chunk uint32

	chunk_size := 4
	random_blob, _ := GetRandomByte(chunk_size)
	chunk = binary.BigEndian.Uint32(random_blob)
	return chunk
}

func RandomUint64() uint64 {
	var chunk uint64

	chunk_size := 8
	random_blob, _ := GetRandomByte(chunk_size)
	chunk = binary.BigEndian.Uint64(random_blob)
	return chunk
}

// handling for incoming miner connections
func onWebsocket(w http.ResponseWriter, r *http.Request) {
	var address string
	var threads int
	if !strings.HasPrefix(r.URL.Path, "/ws/") {
		http.NotFound(w, r)
		return
	}
	url := strings.TrimPrefix(r.URL.Path, "/ws/")
	if strings.Contains(url, "/") {
		values := strings.Split(url, "/")
		address = values[0]
		threadstmp, _ := strconv.Atoi(values[1])
		threads = threadstmp

	} else {
		address = url
		threads = 16
	}

	addr, err := globals.ParseValidateAddress(address)
	if err != nil {
		// Ignore errors for testnet vs. mainnet
		// fmt.Fprintf(w, "err: %s\n", err)
		// return
	}

	upgrader := newUpgrader()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		//panic(err)
		return
	}

	addr_raw := addr.PublicKey.EncodeCompressed()
	wsConn := conn.(*websocket.Conn)

	session := user_session{address: *addr, address_sum: graviton.Sum(addr_raw), threads: threads}
	wsConn.SetSession(&session)

	client_list_mutex.Lock()
	defer client_list_mutex.Unlock()
	client_list[wsConn] = &session
	Wallet_count[client_list[wsConn].address.String()]++
	Address = address
	fmt.Printf("%v Incoming connection: %v, Wallet: %v Threads: %v\n", time.Now().Format(time.Stamp), wsConn.RemoteAddr().String(), address, threads)
}

// forward results to daemon
func newUpgrader() *websocket.Upgrader {
	u := websocket.NewUpgrader()

	u.OnMessage(func(c *websocket.Conn, messageType websocket.MessageType, data []byte) {

		if messageType != websocket.TextMessage {
			return
		}

		client_list_mutex.Lock()
		defer client_list_mutex.Unlock()

		var x MinerInfo_Params
		if json.Unmarshal(data, &x); len(x.Wallet_Address) > 0 {

			if x.Miner_Hashrate > 0 {
				sess := client_list[c]
				sess.hashrate = x.Miner_Hashrate
				client_list[c] = sess
			}

			var NewHashRate float64
			for _, s := range client_list {
				NewHashRate += s.hashrate
			}
			Hashrate = NewHashRate

			// Update miners information
			return
		} else {
			go SendToDaemon(data)
			fmt.Printf("%v Submitting result from miner: %v, Wallet: %v\n", time.Now().Format(time.Stamp), c.RemoteAddr().String(), client_list[c].address.String())
		}
	})

	u.OnClose(func(c *websocket.Conn, err error) {
		client_list_mutex.Lock()
		defer client_list_mutex.Unlock()
		Wallet_count[client_list[c].address.String()]--
		fmt.Printf("%v Lost connection: %v\n", time.Now().Format(time.Stamp), c.RemoteAddr().String())
		delete(client_list, c)
	})

	return u
}

// taken unmodified from derohe repo
// cert handling
func generate_random_tls_cert() tls.Certificate {

	/* RSA can do only 500 exchange per second, we need to be faster
	     * reference https://github.com/golang/go/issues/20058
	    key, err := rsa.GenerateKey(rand.Reader, 512) // current using minimum size
	if err != nil {
	    log.Fatal("Private key cannot be created.", err.Error())
	}

	// Generate a pem block with the private key
	keyPem := pem.EncodeToMemory(&pem.Block{
	    Type:  "RSA PRIVATE KEY",
	    Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	*/
	// EC256 does roughly 20000 exchanges per second
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	b, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		panic(err)
	}
	// Generate a pem block with the private key
	keyPem := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b})

	tml := x509.Certificate{
		SerialNumber: big.NewInt(int64(time.Now().UnixNano())),

		// TODO do we need to add more parameters to make our certificate more authentic
		// and thwart traffic identification as a mass scale

		// you can add any attr that you need
		NotBefore: time.Now().AddDate(0, -1, 0),
		NotAfter:  time.Now().AddDate(1, 0, 0),
		// you have to generate a different serial number each execution
		/*
		   Subject: pkix.Name{
		       CommonName:   "New Name",
		       Organization: []string{"New Org."},
		   },
		   BasicConstraintsValid: true,   // even basic constraints are not required
		*/
	}
	cert, err := x509.CreateCertificate(rand.Reader, &tml, &tml, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}

	// Generate a pem block with the certificate
	certPem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})
	tlsCert, err := tls.X509KeyPair(certPem, keyPem)
	if err != nil {
		panic(err)
	}
	return tlsCert
}
