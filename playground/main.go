// Command playground 提供本地手动实验入口，可在 main 中编写并运行临时代码。
package main

import "fmt"

// main 运行本地 Go 学习示例，用于观察语法和输出结果。
func main() {
	a := "12"
	b := "12"

	if a == b {
		fmt.Println("a==b")
	} else {
		fmt.Println("a!=b")
	}

	for count := 10; count > 0; count-- {
		fmt.Println(count)
	}

	days := 365.2425
	fmt.Printf("Type %T for %[1]v, Type %T for %[2]v\n", days, b)
}
