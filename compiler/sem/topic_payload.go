package sem

import "strings"

func TopicPayloadTypeForTopic(t *Type) (payload *Type, kind string, ok bool) {
	if t != nil && t.Module == "machine.x86_64.topic_payload" && t.Name == "TimerTickTopic" {
		return resolveBuiltinTopicPayload("machine.x86_64.topic_payload", "TimerTickPayload"), "timer_tick", true
	}
	if t != nil && t.Module == "machine.x86_64.topic_u64" && strings.HasPrefix(t.Name, "U64") {
		return primitiveU64Type(), existingU64TopicKind(t), true
	}
	return nil, "", false
}

func resolveBuiltinTopicPayload(moduleName string, typeName string) *Type {
	return &Type{Module: moduleName, Name: typeName, Kind: KindData}
}

func primitiveU64Type() *Type {
	return &Type{Module: "", Name: "U64", Kind: KindPrimitive}
}

func existingU64TopicKind(t *Type) string {
	if t == nil {
		return ""
	}
	switch t.Name {
	case "U64GapTopic":
		return "gap_u64"
	case "U64ReliableTopic":
		return "reliable_u64"
	default:
		return "u64"
	}
}

func payloadLayoutFromType(t *Type) (size uint64, align uint64, ok bool) {
	if t == nil {
		return 0, 0, false
	}
	if t.Kind == KindPrimitive && t.Name == "U64" {
		return 8, 8, true
	}
	if t.Module == "machine.x86_64.topic_payload" && t.Name == "TimerTickPayload" {
		return 24, 8, true
	}
	return 0, 0, false
}
