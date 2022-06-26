package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"derohe-proxy/config"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
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

var proxyConfig config.ProxyConfig
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
	valid_address bool
	address_sum   [32]byte
	threads       int
}

type work_template struct {
	NonceData   [3]uint32
	Flags       uint32
	SharedNonce uint32
}

var client_list_mutex sync.Mutex
var client_list = map[*websocket.Conn]*user_session{}

var miners_count int
var Wallet_count map[string]uint
var Address string

func Start_server(configData config.ProxyConfig) {
	var err error
	proxyConfig = configData

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
	client_list_mutex.Lock()
	defer client_list_mutex.Unlock()
	miners_count = len(client_list)
	return miners_count
}

var random_data_lock sync.Mutex

var MyRandomData []byte

func GetRandomByte(bytes int) ([]byte, error) {
	random_data_lock.Lock()
	defer random_data_lock.Unlock()
	var err error

	picked_data := make([]byte, bytes)
	if len(MyRandomData) > bytes {
		picked_data = MyRandomData[len(MyRandomData)-bytes:]
		MyRandomData = MyRandomData[:len(MyRandomData)-bytes]
	} else {
		err = errors.New("Error Retrieving Random Bytes from Cache")
	}
	return picked_data, err
}

func RandomGenerator() {

	fmt.Print("Generating random data...\n")
	for {

		if len(MyRandomData) < 32768 {

			newdata := make([]byte, 1024)
			start := time.Now()
			byte_size, err := qrand.Read(newdata[:])

			if err == nil {

				random_data_lock.Lock()

				for x, _ := range newdata {
					MyRandomData = append(MyRandomData, newdata[x])
				}

				fmt.Printf("Generated %d bytes of random data in %s (Data store size: %d)\n", byte_size, time.Since(start).String(), len(MyRandomData))

				random_data_lock.Unlock()

			} else {
				fmt.Printf("Error when fetching random data: %s\n", err.Error())
				time.Sleep(time.Second * 5)
			}
		} else {
			// fmt.Print("Sleeping\n")
			time.Sleep(time.Second * 5)
		}

	}

}

func GetGlobalWork() work_template {
	var work_data = work_template{}

	work_data.Flags = 3735928559

	if proxyConfig.Global && proxyConfig.NonceEdit && proxyConfig.ZeroNonce {
		work_data.NonceData[0] = RandomUint32()
		work_data.NonceData[1] = uint32(0)
		work_data.SharedNonce = RandomUint32()
		work_data.NonceData[2] = work_data.SharedNonce
		work_data.Flags = uint32(0)
	} else if proxyConfig.Global && proxyConfig.NonceEdit {
		work_data.NonceData[0] = RandomUint32()
		work_data.NonceData[1] = RandomUint32()
		work_data.SharedNonce = RandomUint32()
		work_data.NonceData[2] = work_data.SharedNonce
		work_data.Flags = RandomUint32()
	}
	return work_data
}

func GetClientWork(work_data work_template, total_threads uint32) work_template {
	var client_data work_template
	if proxyConfig.NonceEdit && proxyConfig.Global {
		noncebytes := make([]byte, 4)
		client_data.NonceData = work_data.NonceData
		client_data.Flags = work_data.Flags
		client_data.NonceData[2] = work_data.SharedNonce + (256 * total_threads)
		binary.BigEndian.PutUint32(noncebytes, client_data.NonceData[2])
		randombytes, err := GetRandomByte(2)
		if err != nil {
			fmt.Println(err)
		}
		copy(noncebytes[2:], randombytes)
		client_data.NonceData[2] = binary.BigEndian.Uint32(noncebytes)
	} else if proxyConfig.NonceEdit && proxyConfig.ZeroNonce {
		_, err := GetRandomByte(1)
		if err != nil {
			fmt.Println(err)
		}
		client_data.NonceData[0] = RandomUint32()
		client_data.NonceData[1] = 0
		client_data.NonceData[2] = RandomUint32()
		client_data.Flags = 0
	} else if proxyConfig.NonceEdit {
		_, err := GetRandomByte(1)
		if err != nil {
			fmt.Println(err)
		}
		client_data.NonceData[0] = RandomUint32()
		client_data.NonceData[1] = RandomUint32()
		client_data.NonceData[2] = RandomUint32()
		client_data.Flags = RandomUint32()
	}
	return client_data
}

func SendTemplateToNode(data []byte, work_data work_template, total_threads uint32, ck *websocket.Conn, cv *user_session) {
	miner_address := cv.address_sum

	if result := edit_blob(data, miner_address, GetClientWork(work_data, total_threads)); result != nil {
		data = result
	} else {
		fmt.Println(time.Now().Format(time.Stamp), "Failed to change nonce / miner keyhash")
	}

	go func(k *websocket.Conn, v *user_session) {
		defer globals.Recover(2)
		k.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
		k.WriteMessage(websocket.TextMessage, data)
	}(ck, cv)
}

// forward all incoming templates from daemon to all miners
func SendTemplateToNodes(data []byte) {
	client_list_mutex.Lock()
	defer client_list_mutex.Unlock()
	var total_threads uint32 = 0

	work_data := GetGlobalWork()

	for rk, rv := range client_list {
		if client_list == nil {
			break
		}
		go SendTemplateToNode(data, work_data, total_threads, rk, rv)
		total_threads += uint32(rv.threads)
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

		SendToDaemon(data)
		fmt.Printf("%v Submitting result from miner: %v, Wallet: %v\n", time.Now().Format(time.Stamp), c.RemoteAddr().String(), client_list[c].address.String())
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
