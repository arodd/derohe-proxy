package proxy

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/deroproject/derohe/block"
)

func edit_blob(input []byte, miner [32]byte, client_data work_template) (output []byte) {
	var err error
	var params GetBlockTemplate_Result
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
	if proxyConfig.NonceEdit {
		for i := range mbl.Nonce {
			mbl.Nonce[i] = client_data.NonceData[i]
		}
	}

	mbl.Flags = client_data.Flags
	//timestamp := uint64(globals.Time().UTC().UnixMilli())
	mbl.Timestamp = uint16(4096) // this will help us better understand network conditions

	params.Blockhashing_blob = fmt.Sprintf("%x", mbl.Serialize())
	encoder := json.NewEncoder(&out)
	if proxyConfig.Verbose {
		line := fmt.Sprintf("Height: %d Difficulty: %s Work %08x%08x%08x%08x", params.Height, params.Difficulty, mbl.Flags, mbl.Nonce[0], mbl.Nonce[1], mbl.Nonce[2])
		fmt.Println(line)
	}
	if err = encoder.Encode(params); err != nil {
		return
	}

	output = out.Bytes()

	return
}
