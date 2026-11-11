// 本文件验证插件新增/删除、生成结果过期、构造约定及生成代码的真实编译注册。
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture 在临时目录创建测试模块，复用真实注册表来验证插件发现和生成过程。
func fixture(t *testing.T) string {
	t.Helper()
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	root := t.TempDir()
	putFixture(t, root, "go.mod", "module example.com/discoveryfixture\n\ngo 1.27.3\n")
	// 复用真实注册表，仅用最小契约隔离与代码生成无关的支付模型。
	registry, err := os.ReadFile("../../internal/plugin/registry.go")
	if err != nil {
		t.Fatal(err)
	}
	putFixture(t, root, "internal/plugin/registry.go", string(registry))
	putFixture(t, root, "internal/plugin/plugin.go", `package plugin
        type Descriptor struct { ID string }
        type Provider interface { Describe() Descriptor }
    `)
	return root
}

// putFixture 将测试源码写入临时模块，目录不存在时先创建目录。
func putFixture(t *testing.T, root, path, source string) {
	t.Helper()
	destination := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
}

// addPlugin 创建指定 ID 的测试插件；所有插件故意使用同一包名，以检查导入别名是否生效。
func addPlugin(t *testing.T, root, name, id string) {
	t.Helper()
	// 两个不同目录故意使用同一包名，验证生成器使用显式别名。
	putFixture(t, root, "internal/plugin/"+name+"/plugin.go", fmt.Sprintf(`package adapter
        import "example.com/discoveryfixture/internal/plugin"
        type Provider struct{}
        func New() *Provider { return &Provider{} }
        func (*Provider) Describe() plugin.Descriptor { return plugin.Descriptor{ID:%q} }
    `, id))
}

// TestDiscoveryLifecycle 验证新增、删除插件后注册代码会正确更新，重复生成结果不变，且生成代码可以编译运行。
func TestDiscoveryLifecycle(t *testing.T) {
	root := fixture(t)
	addPlugin(t, root, "zeta", "zeta")
	if err := run(root, false); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, outputPath))
	if err != nil {
		t.Fatal(err)
	}
	// 新增插件必须被只读检查发现；检查失败不能修改旧生成文件。
	addPlugin(t, root, "alpha", "alpha")
	if err := run(root, true); err == nil {
		t.Fatal("stale registry accepted")
	}
	unchanged, _ := os.ReadFile(filepath.Join(root, outputPath))
	if !bytes.Equal(before, unchanged) {
		t.Fatal("check mode modified generated file")
	}
	if err := run(root, false); err != nil {
		t.Fatal(err)
	}
	generated, _ := os.ReadFile(filepath.Join(root, outputPath))
	if bytes.Index(generated, []byte("/alpha\"")) >= bytes.Index(generated, []byte("/zeta\"")) {
		t.Fatal("plugin imports are not sorted")
	}
	info, _ := os.Stat(filepath.Join(root, outputPath))
	if err := run(root, false); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(filepath.Join(root, outputPath))
	if !after.ModTime().Equal(info.ModTime()) {
		t.Fatal("unchanged generation rewrote the file")
	}
	putFixture(t, root, "internal/plugin/builtin/discovery_test.go", `package builtin
        import "testing"
        // TestRegisteredPlugins 验证生成的注册表实际包含所有发现的插件。
        func TestRegisteredPlugins(t *testing.T) {
            r,err:=NewRegistry()
            if err!=nil { t.Fatal(err) }
            if len(r.IDs())!=2 { t.Fatalf("registered IDs = %v",r.IDs()) }
            for _,id:=range []string{"alpha","zeta"} { if _,ok:=r.Get(id);!ok { t.Fatalf("missing %s",id) } }
        }
    `)
	if _, err := goOutput(root, "test", "./internal/plugin/builtin"); err != nil {
		t.Fatal(err)
	}
	// 删除插件后，即使旧生成文件仍导入它，也必须能重新发现并修复。
	if err := os.RemoveAll(filepath.Join(root, "internal/plugin/alpha")); err != nil {
		t.Fatal(err)
	}
	if err := run(root, false); err != nil {
		t.Fatal(err)
	}
	generated, _ = os.ReadFile(filepath.Join(root, outputPath))
	if bytes.Contains(generated, []byte("/alpha\"")) {
		t.Fatal("deleted plugin remains registered")
	}
	if err := os.RemoveAll(filepath.Join(root, "internal/plugin/zeta")); err != nil {
		t.Fatal(err)
	}
	if err := run(root, false); err != nil {
		t.Fatal(err)
	}
	if _, err := goOutput(root, "build", "./internal/plugin/builtin"); err != nil {
		t.Fatal(err)
	}
}

// TestInvalidPluginsFailClearly 验证构造函数不符合要求、未实现插件接口或插件 ID 重复时会报错，生成失败时不会覆盖旧文件。
func TestInvalidPluginsFailClearly(t *testing.T) {
	cases := []struct {
		name, source    string
		generationFails bool
	}{
		{"missing constructor", `package invalid; type Plugin struct{}`, true},
		{"constructor arguments", `package invalid; func New(value int) int{return value}`, true},
		{"constructor returns error", `package invalid; func New()(int,error){return 1,nil}`, true},
		{"generic constructor", `package invalid; func New[T any]() *T{return nil}`, true},
		{"wrong return interface", `package invalid; func New() int{return 1}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t)
			if err := run(root, false); err != nil {
				t.Fatal(err)
			}
			original, _ := os.ReadFile(filepath.Join(root, outputPath))
			putFixture(t, root, "internal/plugin/invalid/plugin.go", tc.source)
			err := run(root, false)
			if tc.generationFails {
				if err == nil || !strings.Contains(err.Error(), "New") {
					t.Fatalf("invalid constructor: %v", err)
				}
				current, _ := os.ReadFile(filepath.Join(root, outputPath))
				if !bytes.Equal(original, current) {
					t.Fatal("failed generation modified output")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := goOutput(root, "build", "./internal/plugin/builtin"); err == nil {
					t.Fatal("incompatible provider compiled")
				}
			}
		})
	}
	t.Run("duplicate IDs", func(t *testing.T) {
		root := fixture(t)
		addPlugin(t, root, "a", "duplicate")
		addPlugin(t, root, "b", "duplicate")
		if err := run(root, false); err != nil {
			t.Fatal(err)
		}
		putFixture(t, root, "internal/plugin/builtin/discovery_test.go", `package builtin
            import("strings";"testing")
            // TestDuplicate 验证重复插件 ID 在构造注册表时被拒绝。
            func TestDuplicate(t *testing.T){_,err:=NewRegistry();if err==nil||!strings.Contains(err.Error(),"duplicate"){t.Fatalf("expected duplicate error, got %v",err)}}
        `)
		if _, err := goOutput(root, "test", "./internal/plugin/builtin"); err != nil {
			t.Fatal(err)
		}
	})
}
