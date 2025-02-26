/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package queue

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opencensus.io/resource"
	network "knative.dev/networking/pkg"
	"knative.dev/pkg/metrics/metricstest"
	_ "knative.dev/pkg/metrics/testing"
	"knative.dev/serving/pkg/metrics"
)

const targetURI = "http://example.com"

func TestNewRequestMetricsHandlerFailure(t *testing.T) {
	t.Cleanup(reset)
	if _, err := NewRequestMetricsHandler(nil /*next*/, "a", "b", "c", "d", "shøüld fail",
		map[string]string{"testann": "testval"}, map[string]string{"testlab": "testval"}); err == nil {
		t.Error("Should get error when tag value is not ascii")
	}
}

func TestRequestMetricsHandler(t *testing.T) {
	defer reset()
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler, err := NewRequestMetricsHandler(baseHandler, "ns", "svc", "cfg", "rev", "pod",
		map[string]string{"testann": "testval"}, map[string]string{"testlab": "testval"})
	if err != nil {
		t.Fatal("Failed to create handler:", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, targetURI, bytes.NewBufferString("test"))
	handler.ServeHTTP(resp, req)

	wantTags := map[string]string{
		metrics.LabelPodName:           "pod",
		metrics.LabelContainerName:     "queue-proxy",
		metrics.LabelResponseCode:      "200",
		metrics.LabelResponseCodeClass: "2xx",
		"route_tag":                    disabledTagName,
	}
	wantResource := &resource.Resource{
		Type: "knative_revision",
		Labels: map[string]string{
			metrics.LabelNamespaceName:     "ns",
			metrics.LabelRevisionName:      "rev",
			metrics.LabelServiceName:       "svc",
			metrics.LabelConfigurationName: "cfg",
		},
	}

	metricstest.AssertMetricRequiredOnly(t, metricstest.IntMetric("request_count", 1, wantTags).WithResource(wantResource))
	metricstest.AssertMetricRequiredOnly(t, metricstest.DistributionCountOnlyMetric("request_latencies", 1, wantTags).WithResource(wantResource))

	// A probe request should not be recorded.
	req.Header.Set(network.ProbeHeaderName, "activator")
	handler.ServeHTTP(resp, req)
	metricstest.AssertMetricRequiredOnly(t, metricstest.IntMetric("request_count", 1, wantTags).WithResource(wantResource))
	metricstest.AssertMetricRequiredOnly(t, metricstest.DistributionCountOnlyMetric("request_latencies", 1, wantTags).WithResource(wantResource))
}

func TestRequestMetricsHandlerWithEnablingTagOnRequestMetrics(t *testing.T) {
	defer reset()
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler, err := NewRequestMetricsHandler(baseHandler, "ns", "svc", "cfg", "rev", "pod",
		map[string]string{"testann": "testval"}, map[string]string{"testlab": "testval"})
	if err != nil {
		t.Fatal("Failed to create handler:", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, targetURI, bytes.NewBufferString("test"))
	req.Header.Set(network.TagHeaderName, "test-tag")

	handler.ServeHTTP(resp, req)

	wantTags := map[string]string{
		metrics.LabelPodName:           "pod",
		metrics.LabelContainerName:     "queue-proxy",
		metrics.LabelResponseCode:      "200",
		metrics.LabelResponseCodeClass: "2xx",
		metrics.LabelRouteTag:          "test-tag",
	}
	wantResource := &resource.Resource{
		Type: "knative_revision",
		Labels: map[string]string{
			metrics.LabelNamespaceName:     "ns",
			metrics.LabelRevisionName:      "rev",
			metrics.LabelServiceName:       "svc",
			metrics.LabelConfigurationName: "cfg",
		},
	}

	metricstest.AssertMetricRequiredOnly(t, metricstest.IntMetric("request_count", 1, wantTags).WithResource(wantResource))

	// Testing for default route
	reset()
	handler, _ = NewRequestMetricsHandler(baseHandler, "ns", "svc", "cfg", "rev", "pod",
		map[string]string{"testann": "testval"}, map[string]string{"testlab": "testval"})
	req.Header.Del(network.TagHeaderName)
	req.Header.Set(network.DefaultRouteHeaderName, "true")
	handler.ServeHTTP(resp, req)
	wantTags["route_tag"] = defaultTagName
	metricstest.AssertMetricRequiredOnly(t, metricstest.IntMetric("request_count", 1, wantTags).WithResource(wantResource))

	reset()
	handler, _ = NewRequestMetricsHandler(baseHandler, "ns", "svc", "cfg", "rev", "pod",
		map[string]string{"testann": "testval"}, map[string]string{"testlab": "testval"})
	req.Header.Set(network.TagHeaderName, "test-tag")
	req.Header.Set(network.DefaultRouteHeaderName, "true")
	handler.ServeHTTP(resp, req)
	wantTags["route_tag"] = undefinedTagName
	metricstest.AssertMetricRequiredOnly(t, metricstest.IntMetric("request_count", 1, wantTags).WithResource(wantResource))

	reset()
	handler, _ = NewRequestMetricsHandler(baseHandler, "ns", "svc", "cfg", "rev", "pod",
		map[string]string{"testann": "testval"}, map[string]string{"testlab": "testval"})
	req.Header.Set(network.TagHeaderName, "test-tag")
	req.Header.Set(network.DefaultRouteHeaderName, "false")
	handler.ServeHTTP(resp, req)
	wantTags["route_tag"] = "test-tag"
	metricstest.AssertMetricRequiredOnly(t, metricstest.IntMetric("request_count", 1, wantTags).WithResource(wantResource))
}

func reset() {
	metricstest.Unregister(
		requestCountM.Name(), appRequestCountM.Name(),
		responseTimeInMsecM.Name(), appResponseTimeInMsecM.Name(),
		queueDepthM.Name())
}

func TestRequestMetricsHandlerPanickingHandler(t *testing.T) {
	defer reset()
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("no!")
	})
	handler, err := NewRequestMetricsHandler(baseHandler, "ns", "svc", "cfg", "rev", "pod",
		map[string]string{"testann": "testval"}, map[string]string{"testlab": "testval"})
	if err != nil {
		t.Fatal("Failed to create handler:", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, targetURI, bytes.NewBufferString("test"))
	defer func() {
		if err := recover(); err == nil {
			t.Error("Want ServeHTTP to panic, got nothing.")
		}
		wantTags := map[string]string{
			metrics.LabelPodName:           "pod",
			metrics.LabelContainerName:     "queue-proxy",
			metrics.LabelResponseCode:      "500",
			metrics.LabelResponseCodeClass: "5xx",
			"route_tag":                    disabledTagName,
		}
		wantResource := &resource.Resource{
			Type: "knative_revision",
			Labels: map[string]string{
				metrics.LabelNamespaceName:     "ns",
				metrics.LabelRevisionName:      "rev",
				metrics.LabelServiceName:       "svc",
				metrics.LabelConfigurationName: "cfg",
			},
		}
		metricstest.AssertMetricRequiredOnly(t, metricstest.IntMetric("request_count", 1, wantTags).WithResource(wantResource))
		metricstest.AssertMetricRequiredOnly(t, metricstest.DistributionCountOnlyMetric("request_latencies", 1, wantTags).WithResource(wantResource))
	}()
	handler.ServeHTTP(resp, req)
}

func BenchmarkNewRequestMetricsHandler(b *testing.B) {
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	breaker := NewBreaker(BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10})
	handler, err := NewAppRequestMetricsHandler(baseHandler, breaker, "test-ns",
		"test-svc", "test-cfg", "test-rev", "test-pod",
		map[string]string{"testann": "testval"}, map[string]string{"testlab": "testval"})
	if err != nil {
		b.Fatal("failed to create request metric handler:", err)
	}
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, targetURI, nil)

	b.Run("sequential", func(b *testing.B) {
		for j := 0; j < b.N; j++ {
			handler.ServeHTTP(resp, req)
		}
	})

	b.Run("parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				handler.ServeHTTP(resp, req)
			}
		})
	})
}

func TestAppRequestMetricsHandlerPanickingHandler(t *testing.T) {
	defer reset()
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("no!")
	})
	breaker := NewBreaker(BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10})
	handler, err := NewAppRequestMetricsHandler(baseHandler, breaker,
		"ns", "svc", "cfg", "rev", "pod",
		map[string]string{"testann": "testval"}, map[string]string{"testlab": "testval"})
	if err != nil {
		t.Fatal("Failed to create handler:", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, targetURI, bytes.NewBufferString("test"))
	defer func() {
		if err := recover(); err == nil {
			t.Error("Want ServeHTTP to panic, got nothing.")
		}
		wantTags := map[string]string{
			metrics.LabelPodName:           "pod",
			metrics.LabelContainerName:     "queue-proxy",
			metrics.LabelResponseCode:      "500",
			metrics.LabelResponseCodeClass: "5xx",
		}
		wantResource := &resource.Resource{
			Type: "knative_revision",
			Labels: map[string]string{
				metrics.LabelNamespaceName:     "ns",
				metrics.LabelRevisionName:      "rev",
				metrics.LabelServiceName:       "svc",
				metrics.LabelConfigurationName: "cfg",
			},
		}

		metricstest.AssertMetricRequiredOnly(t, metricstest.IntMetric("app_request_count", 1, wantTags).WithResource(wantResource))
		metricstest.AssertMetricRequiredOnly(t, metricstest.DistributionCountOnlyMetric("app_request_latencies", 1, wantTags).WithResource(wantResource))
	}()
	handler.ServeHTTP(resp, req)
}

func TestAppRequestMetricsHandler(t *testing.T) {
	defer reset()
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	breaker := NewBreaker(BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10})
	handler, err := NewAppRequestMetricsHandler(baseHandler, breaker,
		"ns", "svc", "cfg", "rev", "pod",
		map[string]string{"testann": "testval"}, map[string]string{"testlab": "testval"})
	if err != nil {
		t.Fatal("Failed to create handler:", err)
	}

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, targetURI, bytes.NewBufferString("test"))
	handler.ServeHTTP(resp, req)

	wantTags := map[string]string{
		metrics.LabelPodName:           "pod",
		metrics.LabelContainerName:     "queue-proxy",
		metrics.LabelResponseCode:      "200",
		metrics.LabelResponseCodeClass: "2xx",
	}
	wantResource := &resource.Resource{
		Type: "knative_revision",
		Labels: map[string]string{
			metrics.LabelNamespaceName:     "ns",
			metrics.LabelRevisionName:      "rev",
			metrics.LabelServiceName:       "svc",
			metrics.LabelConfigurationName: "cfg",
		},
	}

	metricstest.AssertMetricRequiredOnly(t, metricstest.IntMetric("app_request_count", 1, wantTags).WithResource(wantResource))
	metricstest.AssertMetricRequiredOnly(t, metricstest.DistributionCountOnlyMetric("app_request_latencies", 1, wantTags).WithResource(wantResource))

	// A probe request should not be recorded.
	req.Header.Set(network.ProbeHeaderName, "activator")
	handler.ServeHTTP(resp, req)
	metricstest.AssertMetricRequiredOnly(t, metricstest.IntMetric("app_request_count", 1, wantTags).WithResource(wantResource))
	metricstest.AssertMetricRequiredOnly(t, metricstest.DistributionCountOnlyMetric("app_request_latencies", 1, wantTags).WithResource(wantResource))
}

func BenchmarkRequestMetricsHandler(b *testing.B) {
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler, _ := NewRequestMetricsHandler(baseHandler, "ns", "svc", "cfg", "rev", "pod",
		map[string]string{"testann": "testval"}, map[string]string{"testlab": "testval"})
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

	b.Run("sequential", func(b *testing.B) {
		resp := httptest.NewRecorder()
		for j := 0; j < b.N; j++ {
			handler.ServeHTTP(resp, req)
		}
	})

	b.Run("parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			resp := httptest.NewRecorder()
			for pb.Next() {
				handler.ServeHTTP(resp, req)
			}
		})
	})
}

func BenchmarkAppRequestMetricsHandler(b *testing.B) {
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	breaker := NewBreaker(BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10})
	handler, err := NewAppRequestMetricsHandler(baseHandler, breaker,
		"ns", "svc", "cfg", "rev", "pod",
		map[string]string{"testann": "testval"}, map[string]string{"testlab": "testval"})
	if err != nil {
		b.Fatal("Failed to create handler:", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)

	b.Run("sequential", func(b *testing.B) {
		resp := httptest.NewRecorder()
		for j := 0; j < b.N; j++ {
			handler.ServeHTTP(resp, req)
		}
	})

	b.Run("parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			resp := httptest.NewRecorder()
			for pb.Next() {
				handler.ServeHTTP(resp, req)
			}
		})
	})
}
