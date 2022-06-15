package proxy

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/deroproject/derohe/block"
	"github.com/deroproject/derohe/globals"
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
	timestamp := uint64(globals.Time().UTC().UnixMilli())
	mbl.Timestamp = uint16(timestamp) // this will help us better understand network conditions

	params.Blockhashing_blob = fmt.Sprintf("%x", mbl.Serialize())
	encoder := json.NewEncoder(&out)

	if err = encoder.Encode(params); err != nil {
		return
	}
	fmt.Println("Nonces:", mbl.Nonce[0], mbl.Nonce[1], mbl.Nonce[2], "Flags:", mbl.Flags)
	output = out.Bytes()

	return
}
