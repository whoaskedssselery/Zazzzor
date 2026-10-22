package testkit

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// Gateway opcodes, as in api/openapi.yaml x-gateway.
const (
	gatewayDispatch     = 0
	gatewayHello        = 10
	gatewayHeartbeatAck = 11
)

// CheckGatewayFrame validates one server frame of the gateway against the
// x-gateway section of api/openapi.yaml: the frame envelope, HELLO's
// payload, and a DISPATCH's payload against the schema listed for its
// event type (an event type the contract does not list is an error).
func CheckGatewayFrame(raw []byte) error {
	c, err := loadContract()
	if err != nil {
		return err
	}
	var f struct {
		Op *int            `json:"op"`
		T  *string         `json:"t"`
		S  *int64          `json:"s"`
		D  json.RawMessage `json:"d"`
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("gateway frame is not JSON: %w", err)
	}
	if err = c.checkSchema("GatewayFrame", raw); err != nil {
		return err
	}
	if f.Op == nil {
		return errors.New("gateway frame has no op")
	}
	switch *f.Op {
	case gatewayDispatch:
		if f.T == nil || f.S == nil {
			return fmt.Errorf("DISPATCH frame without t or s: %s", raw)
		}
		name, err := c.gatewayEventSchema(*f.T)
		if err != nil {
			return err
		}
		return c.checkSchema(name, f.D)
	case gatewayHello, gatewayHeartbeatAck:
		if f.T != nil || f.S != nil {
			return fmt.Errorf("op %d frame carries t or s: %s", *f.Op, raw)
		}
		if *f.Op == gatewayHello {
			return c.checkSchema("GatewayHello", f.D)
		}
		if string(f.D) != "null" {
			return fmt.Errorf("HEARTBEAT_ACK d = %s, want null", f.D)
		}
		return nil
	default:
		return fmt.Errorf("unknown server op %d", *f.Op)
	}
}

// gatewayEventSchema returns the components.schemas name x-gateway.events
// lists for an event type.
func (c *contract) gatewayEventSchema(typ string) (string, error) {
	gw, _ := c.doc.Extensions["x-gateway"].(map[string]any)
	evs, _ := gw["events"].(map[string]any)
	ev, ok := evs[typ].(map[string]any)
	if !ok {
		return "", fmt.Errorf("event %s is not listed in x-gateway.events of %s", typ, contractPath)
	}
	ref, _ := ev["$ref"].(string)
	name, ok := strings.CutPrefix(ref, "#/components/schemas/")
	if !ok {
		return "", fmt.Errorf("x-gateway.events.%s must be a $ref to components.schemas", typ)
	}
	return name, nil
}

func (c *contract) checkSchema(name string, raw []byte) error {
	ref, ok := c.doc.Components.Schemas[name]
	if !ok || ref.Value == nil {
		return fmt.Errorf("schema %s is not in %s", name, contractPath)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("%s payload is not JSON: %w", name, err)
	}
	if err := ref.Value.VisitJSON(v, openapi3.EnableJSONSchema2020(), openapi3.MultiErrors()); err != nil {
		return fmt.Errorf("payload does not match schema %s: %w\n%s", name, err, truncate(raw, 2048))
	}
	return nil
}
