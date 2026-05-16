package sem

import "testing"

func TestExecutorTopicKindClassification(t *testing.T) {
	tests := []struct {
		name string
		typ  *Type
		fn   func(*Type) bool
	}{
		{
			name: "executor slot",
			typ:  &Type{Module: "machine.x86_64.cpu_state", Name: "ExecutorSlot", Kind: KindClass},
			fn:   IsExecutorSlotType,
		},
		{
			name: "vcpu",
			typ:  &Type{Module: "machine.x86_64.cpu_state", Name: "Vcpu", Kind: KindClass},
			fn:   IsVcpuType,
		},
		{
			name: "gap topic",
			typ:  &Type{Module: "machine.x86_64.topic_u64", Name: "U64GapTopic", Kind: KindClass},
			fn:   IsTopicType,
		},
		{
			name: "reliable topic",
			typ:  &Type{Module: "machine.x86_64.topic_u64", Name: "U64ReliableTopic", Kind: KindClass},
			fn:   IsTopicType,
		},
		{
			name: "gap publisher",
			typ:  &Type{Module: "machine.x86_64.topic_u64", Name: "U64GapPublisher", Kind: KindClass},
			fn:   IsTopicPublisherType,
		},
		{
			name: "reliable publisher",
			typ:  &Type{Module: "machine.x86_64.topic_u64", Name: "U64ReliablePublisher", Kind: KindClass},
			fn:   IsTopicPublisherType,
		},
		{
			name: "gap subscription",
			typ:  &Type{Module: "machine.x86_64.topic_u64", Name: "U64GapSubscription", Kind: KindClass},
			fn:   IsTopicSubscriptionType,
		},
		{
			name: "reliable subscription",
			typ:  &Type{Module: "machine.x86_64.topic_u64", Name: "U64ReliableSubscription", Kind: KindClass},
			fn:   IsTopicSubscriptionType,
		},
		{
			name: "serial rx topic",
			typ:  &Type{Module: "machine.x86_64.serial", Name: "SerialRxTopic", Kind: KindClass},
			fn:   IsTopicType,
		},
		{
			name: "serial publisher",
			typ:  &Type{Module: "machine.x86_64.serial", Name: "SerialRxPublisher", Kind: KindClass},
			fn:   IsTopicPublisherType,
		},
		{
			name: "serial subscription",
			typ:  &Type{Module: "machine.x86_64.serial", Name: "SerialRxSubscription", Kind: KindClass},
			fn:   IsTopicSubscriptionType,
		},
		{
			name: "edu interrupt topic",
			typ:  &Type{Module: "machine.x86_64.edu", Name: "EduInterruptTopic", Kind: KindClass},
			fn:   IsTopicType,
		},
		{
			name: "edu interrupt publisher",
			typ:  &Type{Module: "machine.x86_64.edu", Name: "EduInterruptPublisher", Kind: KindClass},
			fn:   IsTopicPublisherType,
		},
		{
			name: "edu interrupt subscription",
			typ:  &Type{Module: "machine.x86_64.edu", Name: "EduInterruptSubscription", Kind: KindClass},
			fn:   IsTopicSubscriptionType,
		},
		{
			name: "ivshmem doorbell topic",
			typ:  &Type{Module: "machine.x86_64.ivshmem", Name: "IvshmemDoorbellTopic", Kind: KindClass},
			fn:   IsTopicType,
		},
		{
			name: "ivshmem doorbell publisher",
			typ:  &Type{Module: "machine.x86_64.ivshmem", Name: "IvshmemDoorbellPublisher", Kind: KindClass},
			fn:   IsTopicPublisherType,
		},
		{
			name: "ivshmem doorbell subscription",
			typ:  &Type{Module: "machine.x86_64.ivshmem", Name: "IvshmemDoorbellSubscription", Kind: KindClass},
			fn:   IsTopicSubscriptionType,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !test.fn(test.typ) {
				t.Fatalf("expected %s to be classified", qualifiedTypeName(test.typ))
			}
		})
	}

	shadow := &Type{Module: "user.module", Name: "ExecutorSlot", Kind: KindClass}
	if IsExecutorSlotType(shadow) {
		t.Fatalf("user.module.ExecutorSlot should not be classified as an executor slot")
	}
}
