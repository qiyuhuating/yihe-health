// Package commands maps verified commercial wire values. It does not forward HTTP.
package commands

import (
	"fmt"
	"strings"
)

// Source: DeviceType.java constructors and ShowUtils.java:823-841 in the reference
// bundle. Values below are getType() values, never Java enum identifiers.
const fourG = "C3 C3_PRO M1_A M1_B M1_PRO UE U1 UE_C GW03 X1 X1_B X1_C X1_S X1_A X1_PRO M3_CARE X3 X3D X5 X5_S X5_A X7 X8 X8_A X8_B X8_C HA07X HA07X_AI HA07X_433 HA07X_B"
const gprs = "S1 BY102 K1 BY102_LOC BY102_NO_HR C16 S3 S5 S7 BY007 BY007C SHIJIE_GPRS_BP T007 HA01 JIEMEIRUI_WIFI_BP"

// Wire types are static. Build the lookup once rather than splitting and
// scanning both protocol groups for every command resolution.
var channelByType = func() map[string]string {
	channels := make(map[string]string)
	for _, group := range []struct{ values, channel string }{{fourG, "4g"}, {gprs, "gprs"}} {
		for _, value := range strings.Fields(group.values) {
			channels[value] = group.channel
		}
	}
	return channels
}()

func Channel(typ string) (string, error) {
	if typ == "S6T" {
		return "s6t", nil
	}
	if channel, ok := channelByType[typ]; ok {
		return channel, nil
	}
	return "", fmt.Errorf("unsupported wire device type")
}

// Source: RetrofitHelper.java:574-659. Unknown types/actions fail closed.
func Resolve(typ, action string) (string, string, error) {
	channel, err := Channel(typ)
	if err != nil {
		return "", "", err
	}
	wire := ""
	switch action {
	case "location":
		wire = "get_locationdataonce"
		if channel == "s6t" {
			wire = "cr"
		}
		if typ == "BY102" || typ == "K1" || typ == "BY102_LOC" || typ == "BY102_NO_HR" || typ == "C16" {
			wire = "get_locationdata"
		}
	case "restart", "poweroff":
		wire = action
	case "restore":
		wire = "restore"
		if channel == "s6t" {
			wire = "resetq"
		}
	case "ring":
		if channel != "s6t" {
			wire = "ring"
		}
	case "envvoice":
		if channel == "4g" {
			wire = "get_envvoice"
		}
	case "heart_rate":
		if channel != "s6t" {
			wire = "get_heartratedata"
		}
	}
	if wire == "" {
		return "", "", fmt.Errorf("unsupported action for device channel")
	}
	return channel, wire, nil
}

// A retry is another delivery of the SAME command ID; firmware must deduplicate.
func RetrySafe(action string) bool { return action == "location" || action == "heart_rate" }
