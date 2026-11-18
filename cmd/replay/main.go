package main

import (
	"fmt"
	"os"

	"github.com/mualnaqeeb/account-ledger-core/ledger"
)

func main() {
	eng, reports, err := ledger.RunCanonicalReplay()
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay error: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(ledger.FormatReplayReport(eng, reports))
}
