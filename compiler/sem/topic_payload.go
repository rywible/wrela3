package sem

import "strings"

func TopicPayloadTypeForTopic(t *Type) (payload *Type, kind string, ok bool) {
	if IsTopicType(t) && len(t.TypeArgs) == 1 {
		if t.Name == "ReliableTopic" {
			return t.TypeArgs[0], "reliable", true
		}
		if t.Name == "Topic" {
			return t.TypeArgs[0], "topic", true
		}
	}
	// Compatibility branch removed by Task 20 after source migration.
	if t != nil && t.Module == "machine.x86_64.topic_payload" && t.Name == "TimerTickTopic" {
		return resolveBuiltinTopicPayload("machine.x86_64.topic_payload", "TimerTickPayload"), "timer_tick", true
	}
	// Compatibility branch removed by Task 20 after source migration.
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
	if t.Kind == KindPrimitive {
		return primitivePayloadLayout(t.Name)
	}
	if t.Kind == KindEnum {
		enumSize, enumAlign := uint64(8), uint64(8)
		for _, variant := range t.EnumVariants {
			var offset uint64
			var maxAlign uint64 = 1
			for _, field := range variant.Fields {
				fieldSize, fieldAlign, ok := semanticSizeAlign(field.Type)
				if !ok {
					return 0, 0, false
				}
				offset = alignPayloadOffset(offset, fieldAlign)
				offset += fieldSize
				if fieldAlign > maxAlign {
					maxAlign = fieldAlign
				}
			}
			payload := alignPayloadOffset(offset, maxAlign)
			if 8+payload > enumSize {
				enumSize = 8 + payload
			}
			if maxAlign > enumAlign {
				enumAlign = maxAlign
			}
		}
		return alignPayloadOffset(enumSize, enumAlign), enumAlign, true
	}
	if t.Kind != KindData && t.Kind != KindClass {
		return 0, 0, false
	}
	var offset uint64
	var maxAlign uint64
	for _, field := range t.Fields {
		fieldSize, fieldAlign, ok := payloadLayoutFromType(field.Type)
		if !ok {
			return 0, 0, false
		}
		offset = alignPayloadOffset(offset, fieldAlign)
		offset += fieldSize
		if fieldAlign > maxAlign {
			maxAlign = fieldAlign
		}
	}
	if maxAlign == 0 {
		return 0, 0, false
	}
	return alignPayloadOffset(offset, maxAlign), maxAlign, true
}

func primitivePayloadLayout(name string) (size uint64, align uint64, ok bool) {
	switch name {
	case "Bool", "U8":
		return 1, 1, true
	case "U16":
		return 2, 2, true
	case "U32":
		return 4, 4, true
	case "U64", "I64", "PhysicalAddress", "VirtualAddress":
		return 8, 8, true
	default:
		return 0, 0, false
	}
}

func semanticSizeAlign(t *Type) (size uint64, align uint64, ok bool) {
	if t == nil {
		return 0, 0, false
	}
	if t.Kind == KindPrimitive {
		return primitivePayloadLayout(t.Name)
	}
	if t.Kind != KindData && t.Kind != KindClass {
		return 0, 0, false
	}
	var offset uint64
	var maxAlign uint64 = 1
	for _, field := range t.Fields {
		fieldSize, fieldAlign, ok := semanticSizeAlign(field.Type)
		if !ok {
			return 0, 0, false
		}
		offset = alignPayloadOffset(offset, fieldAlign)
		offset += fieldSize
		if fieldAlign > maxAlign {
			maxAlign = fieldAlign
		}
	}
	return alignPayloadOffset(offset, maxAlign), maxAlign, true
}

func alignPayloadOffset(offset uint64, align uint64) uint64 {
	if align == 0 || offset%align == 0 {
		return offset
	}
	return offset + align - offset%align
}
