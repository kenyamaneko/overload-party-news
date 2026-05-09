package apinewsfake

// Subscriber は受信側のテスト用 fake。
type Subscriber struct {
	broker *Broker
}

func NewSubscriber(broker *Broker) *Subscriber {
	return &Subscriber{broker: broker}
}

// Messages は topic に発行された payload を読み取る channel を返す (複数呼びで fan-out)。
func (s *Subscriber) Messages(topic string) <-chan []byte {
	return s.broker.Subscribe(topic)
}
