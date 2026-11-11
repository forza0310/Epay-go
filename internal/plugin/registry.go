// 本文件维护启动期注册的可信插件，运行期只读，不支持执行上传的源代码。
package plugin

import (
	"fmt"
	"sort"
)

// Registry 按插件 ID 查找实现；构造完成后不再变更。
type Registry struct{ providers map[string]Provider }

// NewRegistry 按 ID 保存传入的插件，拒绝 nil、空 ID 或重复 ID。
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

// Get 按 ID 查找插件，并返回是否找到。
func (r *Registry) Get(id string) (Provider, bool) { p, ok := r.providers[id]; return p, ok }

// IDs 返回按字典顺序排列的插件 ID，便于稳定展示和记录日志。
func (r *Registry) IDs() []string {
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
