package sem

func IsExecutorSlotType(t *Type) bool {
	switch qualifiedTypeName(t) {
	case "machine.x86_64.executor_slot.ExecutorSlot",
		"machine.x86_64.cpu_state.ExecutorSlot":
		return true
	default:
		return false
	}
}

func IsVcpuType(t *Type) bool {
	return qualifiedTypeName(t) == "machine.x86_64.cpu_state.Vcpu"
}

func IsTopicType(t *Type) bool {
	q := qualifiedTypeName(t)
	return t != nil && (q == "machine.x86_64.topic.Topic" || q == "machine.x86_64.topic.ReliableTopic") && len(t.TypeArgs) == 1
}

func IsTopicPublisherType(t *Type) bool {
	q := qualifiedTypeName(t)
	return t != nil && (q == "machine.x86_64.topic.TopicPublisher" || q == "machine.x86_64.topic.ReliablePublisher") && len(t.TypeArgs) == 1
}

func IsTopicSubscriptionType(t *Type) bool {
	q := qualifiedTypeName(t)
	return t != nil && (q == "machine.x86_64.topic.TopicSubscription" || q == "machine.x86_64.topic.ReliableSubscription") && len(t.TypeArgs) == 1
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
