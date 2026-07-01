package hyperliquid

import (
	"fmt"

	hl "github.com/sonirico/go-hyperliquid"
)

// initExchangeClient converts go-hyperliquid constructor panics into errors so
// transient metadata fetch failures do not crash HTTP handlers or trader setup.
func initExchangeClient(build func() *hl.Exchange) (ex *hl.Exchange, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("hyperliquid client initialization failed: %v", r)
		}
	}()
	return build(), nil
}
