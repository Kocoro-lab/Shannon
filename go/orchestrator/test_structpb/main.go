package main

import (
    "fmt"
    "google.golang.org/protobuf/types/known/structpb"
)

func main() {
    m := map[string]interface{}{
        "metrics": []string{"a","b"},
    }
    st, err := structpb.NewStruct(m)
    fmt.Printf("st=%v err=%v\n", st, err)
}
