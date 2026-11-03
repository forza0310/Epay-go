// 本文件维护启动期注册的可信插件，运行期只读，不支持执行上传的源代码。
package plugin

import (
	"fmt"
	"sort"
)

// Registry 按插件 ID 查找实现；构造完成后不再变更。
type Registry struct{ providers map[string]Provider }

// NewRegistry 拒绝空插件与重复 ID，避免后注册插件覆盖既有能力。
func NewRegistry(providers ...Provider) (*Registry, error) {
	registry := &Registry{providers: make(map[string]Provider, len(providers))}
	for _, provider := range providers {
		if provider == nil {
			return nil, fmt.Errorf("nil payment plugin")
		}
		id := provider.Describe().ID
		if id == "" {
			return nil, fmt.Errorf("empty payment plugin ID")
		}
		if _, exists := registry.providers[id]; exists {
			return nil, fmt.Errorf("duplicate payment plugin: %s", id)
		}
		registry.providers[id] = provider
	}
	return registry, nil
}

// Get 返回指定插件，不向调用方暴露可修改的注册映射。
func (r *Registry) Get(id string) (Provider, bool) { p, ok := r.providers[id]; return p, ok }

// IDs 为启动日志返回排序后的插件 ID。
func (r *Registry) IDs() []string {
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
