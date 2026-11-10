// Package builtin 装配构建时发现的可信插件；注册实现由 plugin-gen 生成。
// 插件包依赖 plugin 契约，不能反向导入 builtin，以避免导入环。
package builtin

//go:generate go run ../../../cmd/plugin-gen -root ../../..
