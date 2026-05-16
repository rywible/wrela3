package ir

import "testing"

func TestTopicOpsDefineExpectedValues(t *testing.T) {
	publish := TopicPublish{TopicLabel: "counter", Kind: "gap_u64", Value: ConstInt{Value: 1}}
	tryNext := TopicTryNext{TopicLabel: "counter", Subscription: Local{Symbol: "sub"}, Type: Type{Name: "U64TopicNext"}}
	arm := TopicArmWait{TopicLabel: "counter", Subscription: Local{Symbol: "sub"}}
	reliable := ReliableTopicTryPublish{TopicLabel: "commands", Value: ConstInt{Value: 7}, Type: Type{Name: "U64PublishResult"}}
	ops := []Operation{publish, tryNext, arm, reliable}
	for _, op := range ops {
		if op == nil {
			t.Fatal("nil op")
		}
	}
	if len(valuesDefinedBy(tryNext)) != 1 {
		t.Fatal("TopicTryNext must define a value")
	}
	if len(valuesDefinedBy(reliable)) != 1 {
		t.Fatal("ReliableTopicTryPublish must define a value")
	}
}
