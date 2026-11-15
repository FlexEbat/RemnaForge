package verify

import (
	"fmt"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// sleep is replaced by tests.
var sleep = time.Sleep

// Wait runs check until it passes or attempts are used up, sleeping delay
// between tries. onFail is called after each failed try except the last. It
// returns the result of the last try.
func Wait(check func() Result, attempts int, delay time.Duration, onFail func(attempt int, r Result)) Result {
	var r Result
	for attempt := 1; attempt <= attempts; attempt++ {
		r = check()
		if r.OK {
			return r
		}
		if attempt == attempts {
			break
		}
		if onFail != nil {
			onFail(attempt, r)
		}
		sleep(delay)
	}
	return r
}

// WaitForApp waits for the panel backend after `docker compose up`: 40
// tries, 8 seconds apart, with the progress lines the install flows print.
func WaitForApp(env Env) error {
	const attempts = 40
	r := Wait(func() Result { return AppAnswers(env) }, attempts, 8*time.Second, func(attempt int, _ Result) {
		fmt.Printf(ui.ColorRed+i18n.T("CONTAINERS_NOT_READY_ATTEMPT")+ui.ColorReset+"\n", attempt, attempts)
	})
	if !r.OK {
		return fmt.Errorf(i18n.T("CONTAINERS_TIMEOUT"), attempts)
	}
	return nil
}

// WaitForNode waits for the selfsteal site of a node: 5 tries, 15 seconds
// apart.
func WaitForNode(env Env, domain string) error {
	const attempts = 5
	try := 0
	r := Wait(func() Result {
		try++
		fmt.Printf(ui.ColorYellow+i18n.T("NODE_ATTEMPT")+ui.ColorReset+"\n", try, attempts)
		return SelfstealServed(env, domain)
	}, attempts, 15*time.Second, func(attempt int, _ Result) {
		fmt.Printf(ui.ColorRed+i18n.T("NODE_UNAVAILABLE")+ui.ColorReset+"\n", attempt)
	})
	if r.OK {
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("NODE_LAUNCHED"), ui.ColorReset)
		return nil
	}
	fmt.Printf(ui.ColorRed+i18n.T("NODE_UNAVAILABLE")+ui.ColorReset+"\n", attempts)
	fmt.Printf(ui.ColorRed+i18n.T("NODE_NOT_CONNECTED")+ui.ColorReset+"\n", attempts)
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CHECK_CONFIG"), ui.ColorReset)
	return fmt.Errorf("node not reachable after %d attempts: %s", attempts, r.Detail)
}

// Finish runs the checks of plan at the end of an install flow, prints one
// line per check, saves the report and returns the results. A failed check
// does not undo the install: the containers are up and the operator needs
// the remedy lines, not a rollback.
func Finish(env Env, p Plan) []Result {
	fmt.Printf("\n%s%s%s\n", ui.ColorYellow, i18n.T("VERIFY_TITLE"), ui.ColorReset)
	results := Run(env, p)
	for _, r := range results {
		r.Print()
	}
	if path, err := Save(ReportDir, results, env.Now()); err == nil {
		fmt.Printf("%s%s %s%s\n", ui.ColorGray, i18n.T("REPORT_SAVED"), path, ui.ColorReset)
	} else {
		fmt.Printf("%s%s: %v%s\n", ui.ColorYellow, i18n.T("REPORT_NOT_SAVED"), err, ui.ColorReset)
	}
	return results
}
