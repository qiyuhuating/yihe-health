// 老人位置坐标（device_location 轨迹、device 最新位置、alarm_precess/alarm_process
// 告警坐标）属 PIPL 敏感个人信息：写入时以 securestore AEAD 密文落库，读取时解密。
// 未配置密钥（config.Validate 已在生产阻断该组合）与历史明文行原样读写，无需回填迁移。
// fence_event.lng/lat 是 double 数值列且 API 按数字消费，保持明文（见审计报告 M-3）。
package handlers

import (
	"strings"

	"yihe-care/internal/securestore"
)

const coordinateScope = "location"

// sealCoordinate 加密坐标字符串；空值原样返回。
// crypto/rand.Read 不会失败，Seal 仅在 box 为 nil 时报错，此处已排除。
func sealCoordinate(box *securestore.Box, v string) string {
	if box == nil || v == "" {
		return v
	}
	sealed, _ := box.Seal(v, coordinateScope)
	return sealed
}

// openCoordinate 解密坐标；nil、空值与无密文前缀的历史明文行原样返回。
func openCoordinate(box *securestore.Box, v *string) (*string, error) {
	if v == nil || *v == "" || !strings.HasPrefix(*v, securestore.Prefix) {
		return v, nil
	}
	plain, err := box.Open(*v, coordinateScope)
	if err != nil {
		return nil, err
	}
	return &plain, nil
}

func openCoordinates(box *securestore.Box, lng, lat *string) (*string, *string, error) {
	lng, err := openCoordinate(box, lng)
	if err != nil {
		return nil, nil, err
	}
	lat, err = openCoordinate(box, lat)
	if err != nil {
		return nil, nil, err
	}
	return lng, lat, nil
}

// coordinateArg stores only this request's coordinate. Historical device points
// belong to the independent location snapshot, never the alarm's scene.
func coordinateArg(box *securestore.Box, v string) any {
	if v == "" {
		return nil
	}
	return sealCoordinate(box, v)
}
