package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"sort"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	io2 "github.com/influxdata/influxdb/v2/kit/io"
	"github.com/influxdata/influxdb/v2/models"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/prompb"
)

// HandlerForInfluxLine is a http.Handler which accepts Influx Line protocol and converts it to WriteRequests.
func HandlerForInfluxLine(grpcProxy http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		maxSize := 100 << 10 // TODO: Make this a CLI flag. 100KB for now.

		ts, err := parseInfluxLineReader(r.Context(), r, maxSize)
		if err != nil {
			fmt.Println("error decoding line protocol data", err)
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		rwReq := &prompb.WriteRequest{
			Timeseries: ts,
		}

		data, err := proto.Marshal(rwReq)
		if err != nil {
			fmt.Println("error encoding remote write request", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		compressed := snappy.Encode(nil, data)
		// Copied from: https://github.com/prometheus/prometheus/blob/db445844d3d0521ed352a2a830b281329ba0f34e/storage/remote/client.go#L154
		httpReq, err := http.NewRequest("POST", "http://localhost/api/v1/push", bytes.NewReader(compressed))
		if err != nil {
			fmt.Println("error encoding httpgrpc request", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}
		httpReq.RequestURI = "/api/v1/push" // Need to set this for httpgrpc.
		httpReq.Header.Add("Content-Encoding", "snappy")
		httpReq.Header.Set("Content-Type", "application/x-protobuf")
		httpReq.Header.Set("User-Agent", r.Header.Get("User-Agent"))
		httpReq.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second) // TODO: Make timeout configurable.
		defer cancel()

		httpReq = httpReq.WithContext(ctx)
		grpcProxy.ServeHTTP(w, httpReq)
	})
}

// parseInfluxLineReader parses a Influx Line Protocol request from an io.Reader.
func parseInfluxLineReader(ctx context.Context, r *http.Request, maxSize int) ([]prompb.TimeSeries, error) {
	qp := r.URL.Query()
	precision := qp.Get("precision")
	if precision == "" {
		precision = "ns"
	}

	if !models.ValidPrecision(precision) {
		return nil, fmt.Errorf("precision supplied is not valid: %s", precision)
	}

	encoding := r.Header.Get("Content-Encoding")
	reader, err := batchReadCloser(r.Body, encoding, int64(maxSize))
	if err != nil {
		return nil, err
	}
	data, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	points, err := models.ParsePointsWithPrecision(data, time.Now().UTC(), precision)
	if err != nil {
		return nil, err
	}

	return writeRequestFromInfluxPoints(points)
}

func writeRequestFromInfluxPoints(points []models.Point) ([]prompb.TimeSeries, error) {
	// Technically the same series should not be repeated. We should put all the samples for
	// a series in single client.Timeseries. Having said that doing it is not very optimal and the
	// occurrence of multiple timestamps for the same series is rare. Only reason I see it happening is
	// for backfilling and this is not the API for that. Keeping that in mind, we are going to create a new
	// client.Timeseries for each sample.

	returnTs := []prompb.TimeSeries{}
	for _, pt := range points {
		ts, err := influxPointToTimeseries(pt)
		if err != nil {
			return nil, err
		}
		returnTs = append(returnTs, ts...)
	}

	return returnTs, nil
}

// Points to Prometheus is heavily inspired from https://github.com/prometheus/influxdb_exporter/blob/a1dc16ad596a990d8854545ea39a57a99a3c7c43/main.go#L148-L211
func influxPointToTimeseries(pt models.Point) ([]prompb.TimeSeries, error) {
	returnTs := []prompb.TimeSeries{}

	fields, err := pt.Fields()
	if err != nil {
		return nil, fmt.Errorf("error getting fields from point: %w", err)
	}
	for field, v := range fields {
		var value float64
		switch v := v.(type) {
		case float64:
			value = v
		case int64:
			value = float64(v)
		case bool:
			if v {
				value = 1
			} else {
				value = 0
			}
		default:
			continue
		}

		name := string(pt.Name()) + "_" + field
		if field == "value" {
			name = string(pt.Name())
		}
		replaceInvalidChars(&name)

		tags := pt.Tags()
		lbls := make([]prompb.Label, 0, len(tags)+1) // The additional 1 for __name__.
		lbls = append(lbls, prompb.Label{
			Name:  labels.MetricName,
			Value: name,
		})
		for _, tag := range tags {
			key := string(tag.Key)
			if key == "__name__" {
				continue
			}
			replaceInvalidChars(&key)
			lbls = append(lbls, prompb.Label{
				Name:  key,
				Value: string(tag.Value),
			})
		}
		sort.Slice(lbls, func(i, j int) bool {
			return lbls[i].Name < lbls[j].Name
		})

		returnTs = append(returnTs, prompb.TimeSeries{
			Labels: lbls,
			Samples: []prompb.Sample{{
				Timestamp: util.TimeToMillis(pt.Time()),
				Value:     value,
			}},
		})
	}

	return returnTs, nil
}

// analog of invalidChars = regexp.MustCompile("[^a-zA-Z0-9_]")
func replaceInvalidChars(in *string) {
	for charIndex, char := range *in {
		charInt := int(char)
		if !((charInt >= 97 && charInt <= 122) || // a-z
			(charInt >= 65 && charInt <= 90) || // A-Z
			(charInt >= 48 && charInt <= 57) || // 0-9
			charInt == 95) { // _

			*in = (*in)[:charIndex] + "_" + (*in)[charIndex+1:]
		}
	}
	// prefix with _ if first char is 0-9
	if int((*in)[0]) >= 48 && int((*in)[0]) <= 57 {
		*in = "_" + *in
	}
}

// batchReadCloser (potentially) wraps an io.ReadCloser in Gzip
// decompression and limits the reading to a specific number of bytes.
func batchReadCloser(rc io.ReadCloser, encoding string, maxBatchSizeBytes int64) (io.ReadCloser, error) {
	switch encoding {
	case "gzip", "x-gzip":
		var err error
		rc, err = gzip.NewReader(rc)
		if err != nil {
			return nil, err
		}
	}
	if maxBatchSizeBytes > 0 {
		rc = io2.NewLimitedReadCloser(rc, maxBatchSizeBytes)
	}
	return rc, nil
}
