package kafkax_test

import (
	"context"
	"errors"
	"testing"

	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/fireflg/gophprofile/pkg/kafkax"
	"github.com/fireflg/gophprofile/pkg/kafkax/mocks"
)

const testTopic = "avatars.events"

func newClient(t *testing.T) *mocks.MockMetadataClient {
	t.Helper()

	return mocks.NewMockMetadataClient(gomock.NewController(t))
}

func metadata(topics ...kafka.Topic) *kafka.MetadataResponse {
	return &kafka.MetadataResponse{Brokers: []kafka.Broker{{ID: 1, Host: "localhost", Port: 9092}}, Topics: topics}
}

func TestPingTopicAsksMetadataForTopic(t *testing.T) {
	client := newClient(t)

	client.EXPECT().
		Metadata(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, req *kafka.MetadataRequest) (*kafka.MetadataResponse, error) {
			require.Equal(t, []string{testTopic}, req.Topics)

			return metadata(kafka.Topic{
				Name:       testTopic,
				Partitions: []kafka.Partition{{Topic: testTopic, ID: 0}},
			}), nil
		})

	require.NoError(t, kafkax.PingTopic(t.Context(), client, testTopic))
}

func TestPingTopicWrapsMetadataError(t *testing.T) {
	client := newClient(t)
	metadataErr := errors.New("connection refused")

	client.EXPECT().Metadata(gomock.Any(), gomock.Any()).Return(nil, metadataErr)

	require.ErrorIs(t, kafkax.PingTopic(t.Context(), client, testTopic), metadataErr)
}

func TestPingTopicWrapsTopicError(t *testing.T) {
	client := newClient(t)

	client.EXPECT().Metadata(gomock.Any(), gomock.Any()).Return(metadata(kafka.Topic{
		Name:  testTopic,
		Error: kafka.UnknownTopicOrPartition,
	}), nil)

	require.ErrorIs(t, kafkax.PingTopic(t.Context(), client, testTopic), kafka.UnknownTopicOrPartition)
}

func TestPingTopicRejectsUnusableCluster(t *testing.T) {
	tests := map[string]struct {
		response *kafka.MetadataResponse
		want     string
	}{
		"кластер без брокеров": {
			response: &kafka.MetadataResponse{},
			want:     "no brokers",
		},
		"топик без партиций": {
			response: metadata(kafka.Topic{Name: testTopic}),
			want:     "no partitions",
		},
		"топика нет в кластере": {
			response: metadata(kafka.Topic{
				Name:       "other.topic",
				Partitions: []kafka.Partition{{Topic: "other.topic", ID: 0}},
			}),
			want: "is missing",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			client := newClient(t)

			client.EXPECT().Metadata(gomock.Any(), gomock.Any()).Return(tc.response, nil)

			require.ErrorContains(t, kafkax.PingTopic(t.Context(), client, testTopic), tc.want)
		})
	}
}
