package datadogclient_test

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"github.com/cloudfoundry-incubator/datadog-firehose-nozzle/datadogclient"

	"github.com/cloudfoundry/sonde-go/events"
	"github.com/gogo/protobuf/proto"

	"encoding/json"
	"errors"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"time"
)

var bodies [][]byte

var _ = Describe("DatadogClient", func() {

	var ts *httptest.Server

	BeforeEach(func() {
		bodies = nil
		ts = httptest.NewServer(http.HandlerFunc(handlePost))
	})

	It("ignores messages that aren't value metrics or counter events", func() {
		c := datadogclient.New(ts.URL, "dummykey", "datadog.nozzle.", "test-deployment", "dummy-ip")

		c.AddMetric(&events.Envelope{
			Origin:    proto.String("origin"),
			Timestamp: proto.Int64(1000000000),
			EventType: events.Envelope_LogMessage.Enum(),
			LogMessage: &events.LogMessage{
				Message:     []byte("log message"),
				MessageType: events.LogMessage_OUT.Enum(),
				Timestamp:   proto.Int64(1000000000),
			},
			Deployment: proto.String("deployment-name"),
			Job:        proto.String("doppler"),
		})

		c.AddMetric(&events.Envelope{
			Origin:    proto.String("origin"),
			Timestamp: proto.Int64(1000000000),
			EventType: events.Envelope_ContainerMetric.Enum(),
			ContainerMetric: &events.ContainerMetric{
				ApplicationId: proto.String("app-id"),
				InstanceIndex: proto.Int32(4),
				CpuPercentage: proto.Float64(20.0),
				MemoryBytes:   proto.Uint64(19939949),
				DiskBytes:     proto.Uint64(29488929),
			},
			Deployment: proto.String("deployment-name"),
			Job:        proto.String("doppler"),
		})

		err := c.PostMetrics()
		Expect(err).ToNot(HaveOccurred())

		Eventually(bodies).Should(HaveLen(1))
		var payload datadogclient.Payload
		err = json.Unmarshal(bodies[0], &payload)
		Expect(err).NotTo(HaveOccurred())
		Expect(payload.Series).To(HaveLen(2))

		validateMetrics(payload, 2, 0)
	})

	It("generates aggregate messages even when idle", func() {
		c := datadogclient.New(ts.URL, "dummykey", "datadog.nozzle.", "test-deployment", "dummy-ip")

		err := c.PostMetrics()
		Expect(err).ToNot(HaveOccurred())

		Eventually(bodies).Should(HaveLen(1))
		var payload datadogclient.Payload
		err = json.Unmarshal(bodies[0], &payload)
		Expect(err).NotTo(HaveOccurred())
		Expect(payload.Series).To(HaveLen(2))

		validateMetrics(payload, 0, 0)

		err = c.PostMetrics()
		Expect(err).ToNot(HaveOccurred())

		Eventually(bodies).Should(HaveLen(2))
		err = json.Unmarshal(bodies[1], &payload)
		Expect(err).NotTo(HaveOccurred())
		Expect(payload.Series).To(HaveLen(2))

		validateMetrics(payload, 0, 2)
	})

	It("posts ValueMetrics in JSON format", func() {
		c := datadogclient.New(ts.URL, "dummykey", "datadog.nozzle.", "test-deployment", "dummy-ip")

		c.AddMetric(&events.Envelope{
			Origin:    proto.String("origin"),
			Timestamp: proto.Int64(1000000000),
			EventType: events.Envelope_ValueMetric.Enum(),
			ValueMetric: &events.ValueMetric{
				Name:  proto.String("metricName"),
				Value: proto.Float64(5),
			},
			Deployment: proto.String("deployment-name"),
			Job:        proto.String("doppler"),
		})

		c.AddMetric(&events.Envelope{
			Origin:    proto.String("origin"),
			Timestamp: proto.Int64(2000000000),
			EventType: events.Envelope_ValueMetric.Enum(),
			ValueMetric: &events.ValueMetric{
				Name:  proto.String("metricName"),
				Value: proto.Float64(76),
			},
			Deployment: proto.String("deployment-name"),
			Job:        proto.String("doppler"),
		})

		err := c.PostMetrics()
		Expect(err).ToNot(HaveOccurred())

		Eventually(bodies).Should(HaveLen(1))

		var payload datadogclient.Payload
		err = json.Unmarshal(bodies[0], &payload)
		Expect(err).NotTo(HaveOccurred())
		Expect(payload.Series).To(HaveLen(3))

		metricFound := false
		for _, metric := range payload.Series {
			Expect(metric.Type).To(Equal("gauge"))

			if metric.Metric == "datadog.nozzle.origin.metricName" {
				metricFound = true
				Expect(metric.Points).To(Equal([]datadogclient.Point{
					datadogclient.Point{
						Timestamp: 1,
						Value:     5.0,
					},
					datadogclient.Point{
						Timestamp: 2,
						Value:     76.0,
					},
				}))
				Expect(metric.Tags).To(Equal([]string{"deployment:deployment-name", "job:doppler"}))
			}
		}
		Expect(metricFound).To(BeTrue())

		validateMetrics(payload, 2, 0)
	})

	It("registers metrics with the same name but different tags as different", func() {
		c := datadogclient.New(ts.URL, "dummykey", "datadog.nozzle.", "test-deployment", "dummy-ip")

		c.AddMetric(&events.Envelope{
			Origin:    proto.String("origin"),
			Timestamp: proto.Int64(1000000000),
			EventType: events.Envelope_ValueMetric.Enum(),
			ValueMetric: &events.ValueMetric{
				Name:  proto.String("metricName"),
				Value: proto.Float64(5),
			},
			Deployment: proto.String("deployment-name"),
			Job:        proto.String("doppler"),
		})

		c.AddMetric(&events.Envelope{
			Origin:    proto.String("origin"),
			Timestamp: proto.Int64(2000000000),
			EventType: events.Envelope_ValueMetric.Enum(),
			ValueMetric: &events.ValueMetric{
				Name:  proto.String("metricName"),
				Value: proto.Float64(76),
			},
			Deployment: proto.String("deployment-name"),
			Job:        proto.String("gorouter"),
		})

		err := c.PostMetrics()
		Expect(err).ToNot(HaveOccurred())

		Eventually(bodies).Should(HaveLen(1))

		var payload datadogclient.Payload
		err = json.Unmarshal(bodies[0], &payload)
		Expect(err).NotTo(HaveOccurred())
		Expect(payload.Series).To(HaveLen(4))
		dopplerFound := false
		gorouterFound := false
		for _, metric := range payload.Series {
			Expect(metric.Type).To(Equal("gauge"))

			if metric.Metric == "datadog.nozzle.origin.metricName" {
				Expect(metric.Tags).To(HaveLen(2))
				Expect(metric.Tags[0]).To(Equal("deployment:deployment-name"))
				if metric.Tags[1] == "job:doppler" {
					dopplerFound = true
					Expect(metric.Points).To(Equal([]datadogclient.Point{
						datadogclient.Point{
							Timestamp: 1,
							Value:     5.0,
						},
					}))
				} else if metric.Tags[1] == "job:gorouter" {
					gorouterFound = true
					Expect(metric.Points).To(Equal([]datadogclient.Point{
						datadogclient.Point{
							Timestamp: 2,
							Value:     76.0,
						},
					}))
				} else {
					panic("Unknown tag found")
				}
			}
		}
		Expect(dopplerFound).To(BeTrue())
		Expect(gorouterFound).To(BeTrue())
		validateMetrics(payload, 2, 0)
	})

	It("posts CounterEvents in JSON format and empties map after post", func() {
		c := datadogclient.New(ts.URL, "dummykey", "datadog.nozzle.", "test-deployment", "dummy-ip")

		c.AddMetric(&events.Envelope{
			Origin:    proto.String("origin"),
			Timestamp: proto.Int64(1000000000),
			EventType: events.Envelope_CounterEvent.Enum(),
			CounterEvent: &events.CounterEvent{
				Name:  proto.String("counterName"),
				Delta: proto.Uint64(1),
				Total: proto.Uint64(5),
			},
		})

		c.AddMetric(&events.Envelope{
			Origin:    proto.String("origin"),
			Timestamp: proto.Int64(2000000000),
			EventType: events.Envelope_CounterEvent.Enum(),
			CounterEvent: &events.CounterEvent{
				Name:  proto.String("counterName"),
				Delta: proto.Uint64(6),
				Total: proto.Uint64(11),
			},
		})

		err := c.PostMetrics()
		Expect(err).ToNot(HaveOccurred())

		Eventually(bodies).Should(HaveLen(1))

		var payload datadogclient.Payload
		err = json.Unmarshal(bodies[0], &payload)
		Expect(err).NotTo(HaveOccurred())
		Expect(payload.Series).To(HaveLen(3))
		counterNameFound := false
		for _, metric := range payload.Series {
			Expect(metric.Type).To(Equal("gauge"))

			if metric.Metric == "datadog.nozzle.origin.counterName" {
				counterNameFound = true
				Expect(metric.Points).To(Equal([]datadogclient.Point{
					datadogclient.Point{
						Timestamp: 1,
						Value:     5.0,
					},
					datadogclient.Point{
						Timestamp: 2,
						Value:     11.0,
					},
				}))
			}
		}
		Expect(counterNameFound).To(BeTrue())
		validateMetrics(payload, 2, 0)

		err = c.PostMetrics()
		Expect(err).ToNot(HaveOccurred())

		Eventually(bodies).Should(HaveLen(2))

		err = json.Unmarshal(bodies[1], &payload)
		Expect(err).NotTo(HaveOccurred())
		Expect(payload.Series).To(HaveLen(2))

		validateMetrics(payload, 2, 3)
	})

	It("sends an error metric when consumer error is set", func() {
		c := datadogclient.New(ts.URL, "dummykey", "datadog.nozzle.", "test-deployment", "dummy-ip")

		c.SetSlowConsumerError(errors.New("bad things happened"))

		err := c.PostMetrics()
		Expect(err).ToNot(HaveOccurred())

		Eventually(bodies).Should(HaveLen(1))
		var payload datadogclient.Payload
		err = json.Unmarshal(bodies[0], &payload)
		Expect(err).NotTo(HaveOccurred())
		Expect(payload.Series).To(HaveLen(3))

		errMetric := findSlowConsumerMetric(payload)
		Expect(errMetric).NotTo(BeNil())
		Expect(errMetric.Type).To(Equal("gauge"))
		Expect(errMetric.Points).To(HaveLen(1))
		Expect(errMetric.Points[0].Value).To(BeEquivalentTo(1))
	})

	It("only sends an error metric once", func() {
		c := datadogclient.New(ts.URL, "dummykey", "datadog.nozzle.", "test-deployment", "dummy-ip")

		c.SetSlowConsumerError(errors.New("bad things happened"))

		err := c.PostMetrics()
		Expect(err).ToNot(HaveOccurred())

		Eventually(bodies).Should(HaveLen(1))
		var payload datadogclient.Payload
		err = json.Unmarshal(bodies[0], &payload)
		Expect(err).NotTo(HaveOccurred())

		Expect(findSlowConsumerMetric(payload)).NotTo(BeNil())

		err = c.PostMetrics()
		Expect(err).ToNot(HaveOccurred())

		Eventually(bodies).Should(HaveLen(2))
		err = json.Unmarshal(bodies[1], &payload)
		Expect(err).NotTo(HaveOccurred())

		Expect(findSlowConsumerMetric(payload)).To(BeNil())
	})

})

func validateMetrics(payload datadogclient.Payload, totalMessagesReceived int, totalMetricsSent int) {
	totalMessagesReceivedFound := false
	totalMetricsSentFound := false
	for _, metric := range payload.Series {
		Expect(metric.Type).To(Equal("gauge"))

		internalMetric := false
		var metricValue int
		if metric.Metric == "datadog.nozzle.totalMessagesReceived" {
			totalMessagesReceivedFound = true
			internalMetric = true
			metricValue = totalMessagesReceived
		}
		if metric.Metric == "datadog.nozzle.totalMetricsSent" {
			totalMetricsSentFound = true
			internalMetric = true
			metricValue = totalMetricsSent
		}

		if internalMetric {
			Expect(metric.Points).To(HaveLen(1))
			Expect(metric.Points[0].Timestamp).To(BeNumerically(">", time.Now().Unix()-10), "Timestamp should not be less than 10 seconds ago")
			Expect(metric.Points[0].Value).To(Equal(float64(metricValue)))
			Expect(metric.Tags).To(Equal([]string{"ip:dummy-ip", "deployment:test-deployment"}))
		}
	}
	Expect(totalMessagesReceivedFound).To(BeTrue())
	Expect(totalMetricsSentFound).To(BeTrue())
}

func handlePost(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		panic("No body!")
	}

	bodies = append(bodies, body)
}

func findSlowConsumerMetric(payload datadogclient.Payload) *datadogclient.Metric {
	for _, metric := range payload.Series {
		if metric.Metric == "datadog.nozzle.restartsFromSlowNozzle" {
			return &metric
		}
	}
	return nil
}
