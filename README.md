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

## TODO
1. Handle identifiers present in If
2. Handle identifiers in switch
3. Check if the arg is passed as read only in function args
  * Currently if an identifier is present in func args, we ignore
