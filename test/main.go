package something

import "fmt"

func A() {
	// Cannot be moved to global. It's reassigned
	a := map[string]string{}
	a = nil

	d := "a"
	c := []string{}

	var abcd string

	b := a["1"]
	b = "1234" + "12" + d
	fmt.Println(b, abcd, c)
}

type server struct{}

func (s *server) A() {
	// This a can be moved to global
	a := map[string]string{}

	b := a["1"]
	Do(b)
	fmt.Println(b)
}

func Do[T any](t T) {
}
