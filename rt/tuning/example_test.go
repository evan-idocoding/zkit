package tuning_test

import (
	"fmt"

	"github.com/evan-idocoding/zkit/rt/tuning"
)

func Example_basic() {
	tu := tuning.New()
	enabled, _ := tu.Bool("feature.x", false)

	fmt.Println(enabled.Get(), enabled.Source())

	_ = tu.SetFromString("feature.x", "on")
	fmt.Println(enabled.Get(), enabled.Source())

	// Output:
	// false default
	// true runtime-set
}

func ExampleTuning_ExportOverridesJSON() {
	tu := tuning.New()
	b, _ := tu.Bool("b", false)
	_ = b.Set(true)

	bs, _ := tu.ExportOverridesJSON()
	fmt.Println(string(bs))

	// Output:
	// [{"key":"b","type":"bool","value":"true"}]
}

func ExampleWithOnChangeBool() {
	tu := tuning.New()

	calls := 0
	b, _ := tu.Bool("b", false, tuning.WithOnChangeBool(func(v bool) {
		calls++
		fmt.Printf("onChange: %v\n", v)
	}))

	// onChange is executed synchronously inside Set, after the value is applied.
	// It runs even if the new value equals the current value.
	_ = b.Set(false)
	_ = b.Set(true)
	fmt.Println("calls:", calls)

	// Output:
	// onChange: false
	// onChange: true
	// calls: 2
}
