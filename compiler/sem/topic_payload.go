package sem

func TopicPayloadTypeForTopic(t *Type) (payload *Type, kind string, ok bool) {
	if IsTopicType(t) && len(t.TypeArgs) == 1 {
		return t.TypeArgs[0], topicKindFromPayload(t.TypeArgs[0], t.Name == "ReliableTopic"), true
	}
	return nil, "", false
}

func topicKindFromPayload(payload *Type, reliable bool) string {
	if payload == nil {
		if reliable {
			return "reliable"
		}
		return "topic"
	}
	switch payload.Key() {
	case "U64":
		if reliable {
			return "reliable_u64"
		}
		return "gap_u64"
	case "machine.x86_64.topic_payload.TimerTickPayload":
		return "timer_tick"
	case "machine.x86_64.topic_payload.SerialPathInterrupt":
		return "serial_rx"
	case "machine.x86_64.edu.EduInterrupt":
		return "edu_interrupt"
	case "machine.x86_64.ivshmem.IvshmemDoorbellInterrupt":
		return "ivshmem_doorbell"
	default:
		if reliable {
			return "reliable"
		}
		return "topic"
	}
}

func resolveBuiltinTopicPayload(moduleName string, typeName string) *Type {
	return &Type{Module: moduleName, Name: typeName, Kind: KindData}
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
