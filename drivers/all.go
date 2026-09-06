// Package drivers 通过空导入聚合所有驱动，触发各驱动的 init() 注册。
package drivers

import (
	_ "litepan/drivers/115_Open"
	_ "litepan/drivers/123_Open"
	_ "litepan/drivers/139Cloud"
	_ "litepan/drivers/189Cloud"
	_ "litepan/drivers/Baidu_Open"
	_ "litepan/drivers/Guangya"
	_ "litepan/drivers/LocalFs"
	_ "litepan/drivers/OneDrive"
	_ "litepan/drivers/OpenList"
	_ "litepan/drivers/Quark"
	_ "litepan/drivers/WebDAV"
)
