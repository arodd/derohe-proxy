package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
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

var client_list_mutex sync.Mutex
var client_list = map[*websocket.Conn]*user_session{}

var miners_count int
var Wallet_count map[string]uint
var Address string

func Start_server(listen string) {
	var err error

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{generate_random_tls_cert()},
		InsecureSkipVerify: true,
	}

	mux := &http.ServeMux{}
	mux.HandleFunc("/", onWebsocket) // handle everything

	server = nbhttp.NewServer(nbhttp.Config{
		Name:                    "GETWORK",
		Network:                 "tcp",
		AddrsTLS:                []string{listen},
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

		if len(MyRandomData) < 8192 {

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

func SendTemplateToNode() {

}

// forward all incoming templates from daemon to all miners
func SendTemplateToNodes(data []byte, nonce bool, zero bool, global bool, verbose bool) {

	client_list_mutex.Lock()
	defer client_list_mutex.Unlock()
	//fmt.Println(client_nonces)
	var noncedata [3]uint32
	var sharednonce uint32
	var flags uint32
	var i uint32 = 0
	flags = 3735928559
	if global && nonce && zero {
		noncedata[0] = RandomUint32()
		noncedata[1] = uint32(0)
		sharednonce = RandomUint32()
		noncedata[2] = sharednonce
		flags = uint32(0)
	} else if global && nonce {
		noncedata[0] = RandomUint32()
		noncedata[1] = RandomUint32()
		sharednonce = RandomUint32()
		noncedata[2] = sharednonce
		flags = RandomUint32()
	}
	for rk, rv := range client_list {

		if client_list == nil {
			break
		}
		if nonce && global {
			noncebytes := make([]byte, 4)
			noncedata[2] = sharednonce + (65536 * (i * uint32(rv.threads)))
			binary.BigEndian.PutUint32(noncebytes, noncedata[2])
			randombytes, err := GetRandomByte(2)
			if err != nil {
				fmt.Println(err)
			}
			copy(noncebytes[2:], randombytes)
			noncedata[2] = binary.BigEndian.Uint32(noncebytes)
		} else if nonce && zero {
			_, err := GetRandomByte(1)
			if err != nil {
				fmt.Println(err)
			}
			noncedata[0] = RandomUint32()
			noncedata[1] = 0
			noncedata[2] = RandomUint32()
			flags = 0
		} else if nonce {
			_, err := GetRandomByte(1)
			if err != nil {
				fmt.Println(err)
			}
			noncedata[0] = RandomUint32()
			noncedata[1] = RandomUint32()
			noncedata[2] = RandomUint32()
			flags = RandomUint32()
		}

		miner_address := rv.address_sum

		if result := edit_blob(data, miner_address, nonce, verbose, noncedata, flags); result != nil {
			data = result
		} else {
			fmt.Println(time.Now().Format(time.Stamp), "Failed to change nonce / miner keyhash")
		}

		go func(k *websocket.Conn, v *user_session) {
			defer globals.Recover(2)
			k.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
			k.WriteMessage(websocket.TextMessage, data)
		}(rk, rv)
		i++
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
