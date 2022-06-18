package proxy

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/deroproject/derohe/block"
	"github.com/deroproject/derohe/globals"
	"github.com/deroproject/derohe/rpc"
)

func edit_blob(input []byte, miner [32]byte, nonce bool, noncedata [3]uint32, flags uint32) (output []byte) {
	var err error
	var params rpc.GetBlockTemplate_Result
	var mbl block.MiniBlock
	var raw_hex []byte
	var out bytes.Buffer

	if err = json.Unmarshal(input, &params); err != nil {
		return
	}

	if raw_hex, err = hex.DecodeString(params.Blockhashing_blob); err != nil {
		return
	}

	if mbl.Deserialize(raw_hex); err != nil {
		return
	}

	// Insert miner address
	if !mbl.Final {
		copy(mbl.KeyHash[:], miner[:])
	}

	// Insert random nonce
	if nonce {
		for i := range mbl.Nonce {
			mbl.Nonce[i] = noncedata[i]
		}
	}

	mbl.Flags = flags
	timestamp := uint64(globals.Time().UTC().UnixMilli())
	mbl.Timestamp = uint16(timestamp) // this will help us better understand network conditions

	params.Blockhashing_blob = fmt.Sprintf("%x", mbl.Serialize())
	encoder := json.NewEncoder(&out)
	fmt.Println("Nonces:", mbl.Nonce[0], mbl.Nonce[1], mbl.Nonce[2], "Flags:", mbl.Flags)
	if err = encoder.Encode(params); err != nil {
		return
	}

	output = out.Bytes()

	return
}
