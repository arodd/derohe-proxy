package proxy

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"crypto/rand"

	"github.com/deroproject/derohe/block"
	"github.com/deroproject/derohe/rpc"
)

func edit_blob(input []byte) (output []byte) {
	var err error
	var params rpc.GetBlockTemplate_Result
	var mbl block.MiniBlock
	var raw_hex []byte
	var out bytes.Buffer
	var val int

	if err = json.Unmarshal(input, &params); err != nil {
		return
	}

	if raw_hex, err = hex.DecodeString(params.Blockhashing_blob); err != nil {
		return
	}

	if mbl.Deserialize(raw_hex); err != nil {
		return
	}
	key := [4]byte{}

	for i := range mbl.Nonce {
		val, err = rand.Read(key[:])
		if err != nil {
		    panic(err)
		}
		mbl.Nonce[i] = uint32(val)
	}
	val, err = rand.Read(key[:])
	if err != nil {
            panic(err)
        }
	mbl.Flags = uint32(val)

	params.Blockhashing_blob = fmt.Sprintf("%x", mbl.Serialize())
	encoder := json.NewEncoder(&out)

	if err = encoder.Encode(params); err != nil {
		return
	}

	output = out.Bytes()

	return
}
