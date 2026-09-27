package api

import (
	"reflect"
	"testing"
)

func TestNormalizePanSouRenameTargets(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "空值", raw: "", want: nil},
		{name: "历史布尔关闭", raw: "false", want: nil},
		{name: "历史布尔关闭大写", raw: "FALSE", want: nil},
		{name: "历史布尔开启等价全选", raw: "true", want: []string{"quark", "115", "magnet"}},
		{name: "逗号分隔多选", raw: "quark,115", want: []string{"quark", "115"}},
		{name: "去重", raw: "quark,quark,magnet", want: []string{"quark", "magnet"}},
		{name: "过滤无效代号", raw: "quark,baidu,ed2k", want: []string{"quark"}},
		{name: "分号与空白分隔", raw: " quark; 115 magnet ", want: []string{"quark", "115", "magnet"}},
		{name: "全部无效为空", raw: "baidu,ed2k", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizePanSouRenameTargets(tt.raw)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("normalizePanSouRenameTargets(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestNormalizePanSouRenameTargetList(t *testing.T) {
	if got := normalizePanSouRenameTargetList(nil); got != nil {
		t.Fatalf("空列表应返回 nil, got %v", got)
	}
	if got := normalizePanSouRenameTargetList([]string{"quark", "115", "quark"}); !reflect.DeepEqual(got, []string{"quark", "115"}) {
		t.Fatalf("列表去重失败: %v", got)
	}
	if got := normalizePanSouRenameTargetList([]string{"magnet", "baidu"}); !reflect.DeepEqual(got, []string{"magnet"}) {
		t.Fatalf("列表过滤无效代号失败: %v", got)
	}
}
