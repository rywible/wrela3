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
	return payloadLayoutFromTypeSeen(t, map[string]bool{})
}

func payloadLayoutFromTypeSeen(t *Type, visiting map[string]bool) (size uint64, align uint64, ok bool) {
	_, align, size, ok = semanticSizeAlignAndStorageSeen(t, visiting)
	if !ok {
		return 0, 0, false
	}
	return size, align, true
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
	return semanticSizeAlignSeen(t, map[string]bool{})
}

func semanticSizeAlignSeen(t *Type, visiting map[string]bool) (size uint64, align uint64, ok bool) {
	_, align, size, ok = semanticSizeAlignAndStorageSeen(t, visiting)
	if !ok {
		return 0, 0, false
	}
	return size, align, true
}

type semanticFieldLayout struct {
	valueSize   uint64
	valueAlign  uint64
	storageSize uint64
	isData      bool
}

func semanticSizeAlignAndStorageSeen(t *Type, visiting map[string]bool) (valueSize uint64, align uint64, storageSize uint64, ok bool) {
	if t == nil {
		return 0, 0, 0, false
	}
	if t.Kind == KindPrimitive {
		size, align, ok := primitivePayloadLayout(t.Name)
		if !ok {
			return 0, 0, 0, false
		}
		return size, align, size, true
	}
	key := t.Key()
	if visiting[key] {
		return 0, 0, 0, false
	}
	visiting[key] = true
	defer delete(visiting, key)

	if t.Kind == KindEnum {
		enumSize, enumAlign := uint64(8), uint64(8)
		for _, variant := range t.EnumVariants {
			variantSize, variantAlign, ok := semanticVariantPayloadSize(variant.Fields, visiting)
			if !ok {
				return 0, 0, 0, false
			}
			if 8+variantSize > enumSize {
				enumSize = 8 + variantSize
			}
			if variantAlign > enumAlign {
				enumAlign = variantAlign
			}
		}
		enumSize = alignPayloadOffset(enumSize, enumAlign)
		if enumSize == 0 {
			enumSize = 8
		}
		return enumSize, enumAlign, enumSize, true
	}
	if t.Kind != KindData && t.Kind != KindClass {
		return 0, 0, 0, false
	}
	if len(t.Fields) == 0 {
		return 8, 8, 8, true
	}

	fieldLayouts := make([]semanticFieldLayout, 0, len(t.Fields))
	for _, field := range t.Fields {
		fieldLayout, fieldOk := semanticFieldSizeAndStorage(field.Type, visiting)
		if !fieldOk {
			return 0, 0, 0, false
		}
		fieldLayout.isData = field.Type != nil && field.Type.Kind == KindData
		valueSize = alignPayloadOffset(valueSize, fieldLayout.valueAlign)
		valueSize += fieldLayout.valueSize
		if fieldLayout.valueAlign > align {
			align = fieldLayout.valueAlign
		}
		fieldLayouts = append(fieldLayouts, fieldLayout)
	}
	if align == 0 {
		align = 8
	}
	valueSize = alignPayloadOffset(valueSize, align)

	storageOffset := valueSize
	for _, fieldLayout := range fieldLayouts {
		if !fieldLayout.isData {
			continue
		}
		storageAlign := fieldLayout.valueAlign
		if storageAlign == 0 {
			storageAlign = 8
		}
		storageOffset = alignPayloadOffset(storageOffset, storageAlign)
		storageOffset += fieldLayout.storageSize
	}
	storageSize = alignPayloadOffset(storageOffset, align)
	if storageSize == 0 {
		storageSize = valueSize
	}
	return valueSize, align, storageSize, true
}

func semanticVariantPayloadSize(fields []Field, visiting map[string]bool) (payloadSize uint64, payloadAlign uint64, ok bool) {
	var offset uint64 = 8
	payloadAlign = 1
	for _, field := range fields {
		fieldLayout, fieldOk := semanticFieldSizeAndStorage(field.Type, visiting)
		if !fieldOk {
			return 0, 0, false
		}
		offset = alignPayloadOffset(offset, fieldLayout.valueAlign)
		offset += fieldLayout.storageSize
		if fieldLayout.valueAlign > payloadAlign {
			payloadAlign = fieldLayout.valueAlign
		}
	}
	payloadSize = alignPayloadOffset(offset-8, payloadAlign)
	return payloadSize, payloadAlign, true
}

func semanticFieldSizeAndStorage(fieldType *Type, visiting map[string]bool) (layout semanticFieldLayout, ok bool) {
	if fieldType == nil {
		return layout, false
	}
	if isSemanticHandleValue(fieldType) {
		layout.valueSize = 8
		layout.valueAlign = 8
		_, _, layout.storageSize, ok = semanticSizeAlignAndStorageSeen(fieldType, visiting)
		return layout, ok
	}
	layout.valueSize, layout.valueAlign, layout.storageSize, ok = semanticSizeAlignAndStorageSeen(fieldType, visiting)
	return layout, ok
}

func isSemanticHandleValue(fieldType *Type) bool {
	if fieldType == nil {
		return false
	}
	switch fieldType.Kind {
	case KindData, KindClass, KindDriver, KindDriverPath, KindExecutor:
		return true
	}
	return false
}

func alignPayloadOffset(offset uint64, align uint64) uint64 {
	if align == 0 || offset%align == 0 {
		return offset
	}
	return offset + align - offset%align
}
