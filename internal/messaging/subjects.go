package messaging

import "fmt"

// Subject construction matching NovaCore gateway topic format.
// Pattern: gateways.{serial}.devices.{deviceID}.ders.{derType}.{channel}.json.v1

func TelemetrySubject(gwSerial, deviceID, derType string) string {
	return fmt.Sprintf("gateways.%s.devices.%s.ders.%s.telemetry.json.v1", gwSerial, deviceID, derType)
}

func ControlCommandsSubject(gwSerial, deviceID, derType string) string {
	return fmt.Sprintf("gateways.%s.devices.%s.ders.%s.control.ems.commands", gwSerial, deviceID, derType)
}

func ControlAckSubject(gwSerial, deviceID, derType string) string {
	return fmt.Sprintf("gateways.%s.devices.%s.ders.%s.control.ems.ack", gwSerial, deviceID, derType)
}

func ControlHeartbeatSubject(gwSerial, deviceID, derType string) string {
	return fmt.Sprintf("gateways.%s.devices.%s.ders.%s.control.ems.heartbeat", gwSerial, deviceID, derType)
}

func GatewayStateSubject(gwSerial string) string {
	return fmt.Sprintf("gateways.%s.state.json.v1", gwSerial)
}

func GatewayEventsSubject(gwSerial string) string {
	return fmt.Sprintf("gateways.%s.events", gwSerial)
}

// HuginProbeSubject is the NATS request-reply subject for Hugin probes.
func HuginProbeSubject(gwSerial string) string {
	return fmt.Sprintf("gateways.%s.hugin.probe", gwSerial)
}
