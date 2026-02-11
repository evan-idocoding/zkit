package safego_test

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/evan-idocoding/zkit/rt/safego"
)

func ExampleGoErr_withWaitGroup() {
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(1)

	safego.GoErr(ctx, func(context.Context) error {
		// ... do background work ...
		return nil
	}, safego.WithName("cache-refresh"),
		safego.WithFinally(wg.Done),
		safego.WithErrorHandler(func(context.Context, safego.ErrorInfo) {}),
		safego.WithPanicHandler(func(context.Context, safego.PanicInfo) {}),
	)

	wg.Wait()
	// Output:
}

func ExampleRunErr_reportContextCancel() {
	ctx := context.Background()

	safego.RunErr(ctx, func(context.Context) error {
		return context.Canceled
	}, safego.WithName("worker"),
		safego.WithReportContextCancel(true),
		safego.WithErrorHandler(func(_ context.Context, info safego.ErrorInfo) {
			fmt.Printf("name=%s err=%v\n", info.Name, info.Err)
		}),
	)

	// Output:
	// name=worker err=context canceled
}

func ExampleRunErr_repanicAfterReport() {
	defer func() {
		if p := recover(); p != nil {
			fmt.Printf("panicked: %v\n", p)
		}
	}()

	safego.RunErr(context.Background(), func(context.Context) error {
		panic("boom")
	}, safego.WithPanicPolicy(safego.RepanicAfterReport),
		safego.WithPanicHandler(func(context.Context, safego.PanicInfo) {
			fmt.Println("reported")
		}),
	)

	// Output:
	// reported
	// panicked: boom
}

func ExampleRunErr_errorReporting() {
	ctx := context.Background()

	safego.RunErr(ctx, func(context.Context) error {
		return errors.New("oops")
	}, safego.WithName("job"),
		safego.WithTags(safego.Tag{Key: "k", Value: "v"}),
		safego.WithErrorHandler(func(context.Context, safego.ErrorInfo) {
			fmt.Println("handled")
		}),
	)

	// Output:
	// handled
}
