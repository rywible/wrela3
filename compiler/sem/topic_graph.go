package sem

func IsExecutorSlotType(t *Type) bool {
	return qualifiedTypeName(t) == "machine.x86_64.cpu_state.ExecutorSlot"
}

func IsVcpuType(t *Type) bool {
	return qualifiedTypeName(t) == "machine.x86_64.cpu_state.Vcpu"
}

func IsTopicType(t *Type) bool {
	switch qualifiedTypeName(t) {
	case "machine.x86_64.topic_u64.U64GapTopic",
		"machine.x86_64.topic_u64.U64ReliableTopic",
		"machine.x86_64.topic_payload.TimerTickTopic",
		"machine.x86_64.serial.SerialRxTopic",
		"machine.x86_64.edu.EduInterruptTopic",
		"machine.x86_64.ivshmem.IvshmemDoorbellTopic":
		return true
	default:
		return false
	}
}

func IsTopicPublisherType(t *Type) bool {
	switch qualifiedTypeName(t) {
	case "machine.x86_64.topic_u64.U64GapPublisher",
		"machine.x86_64.topic_u64.U64ReliablePublisher",
		"machine.x86_64.topic_payload.TimerTickPublisher",
		"machine.x86_64.serial.SerialRxPublisher",
		"machine.x86_64.edu.EduInterruptPublisher",
		"machine.x86_64.ivshmem.IvshmemDoorbellPublisher":
		return true
	default:
		return false
	}
}

func IsTopicSubscriptionType(t *Type) bool {
	switch qualifiedTypeName(t) {
	case "machine.x86_64.topic_u64.U64GapSubscription",
		"machine.x86_64.topic_u64.U64ReliableSubscription",
		"machine.x86_64.topic_payload.TimerTickSubscription",
		"machine.x86_64.serial.SerialRxSubscription",
		"machine.x86_64.edu.EduInterruptSubscription",
		"machine.x86_64.ivshmem.IvshmemDoorbellSubscription":
		return true
	default:
		return false
	}
}

func IsLoopPolicyType(t *Type) bool {
	switch qualifiedTypeName(t) {
	case "machine.x86_64.executor_loop.HotPollPolicy",
		"machine.x86_64.executor_loop.EventSleepPolicy",
		"machine.x86_64.executor_loop.AdaptiveLoopPolicy",
		"machine.x86_64.executor_loop.TimerSleepPolicy":
		return true
	default:
		return false
	}
}
