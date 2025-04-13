package something

import "fmt"

func A() {
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
	a := map[string]string{}

	b := a["1"]
	Do(b)
	fmt.Println(b)
}

func Do[T any](t T) {
}
