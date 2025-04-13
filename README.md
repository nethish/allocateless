# Allocate Less
A go linter to report variables in your functions that can be made global

Example
```go
func DoSomething(key string) {
  // This variable can be made global so that your program allocates less variables
  a := map[string]int {
    "a": 2,
  }

  return 2 * a[key]

}
```


## Install

```bash
go install github.com/nethish/allocateless@latest
allocateless ./...
```
