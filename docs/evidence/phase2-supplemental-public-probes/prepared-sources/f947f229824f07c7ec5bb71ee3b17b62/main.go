package main
import("encoding/json";"os";"fmt")
func main(){var inputs []map[string]any;if e:=json.Unmarshal([]byte(os.Args[1]),&inputs);e!=nil{panic(e)};out:=[]any{};for _,x:=range inputs{out=append(out,solve(x))};b,e:=json.Marshal(out);if e!=nil{panic(e)};fmt.Println(string(b))}
