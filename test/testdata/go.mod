// https://github.com/golang/go/issues/26672#issuecomment-409685692
// Without this file go 1.13 mod cannot get cpu-pooler as dependency
// Error: malformed file path ...: double dot
// Fixed in v1.14
module github.com/nokia/CPU-Pooler

go 1.13
