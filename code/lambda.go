package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/logicmonitor/lm-logs-sdk-go/ingest"
)

var lmHost, awsRegion, scrubRegex string
var accessID, accessKey string
var debug bool

func SendLogs(logs []ingest.Log) {
	r := regexp.MustCompile(`https://([^\.]*).logicmonitor.com`)
	result := r.FindStringSubmatch(lmHost)
	company := result[1]

	lmIngest := ingest.Ingest{
		CompanyName: company,
		AccessID:    accessID,
		AccessKey:   accessKey,
	}

	// Send logs to Logic Monitor
	ingestResponse, err := lmIngest.SendLogs(logs)
	handleFatalError("Request failed", err)

	if debug || !ingestResponse.Success {
		json, _ := json.Marshal(ingestResponse)
		fmt.Printf("Response: %s\n", string(json))
		fmt.Println(string(json))
	}
}

func ScrubLogsWithRegex(lmBatch []ingest.Log) {
	if scrubRegex != "" {
		reg := regexp.MustCompile(scrubRegex)
		for _, event := range lmBatch {
			log.Print(event.Message)
			event.Message = reg.ReplaceAllString(event.Message, "")
			log.Print(event.Message)
		}
	}
}

func ParseEventType(requests interface{}) string {
	data := requests.(map[string]interface{})

	_, ok := data["awslogs"]
	if ok {
		return "cloudwatch"
	}

	_, ok = data["Records"]
	if ok {
		event := convertToS3Event(requests)
		if strings.Contains(event.Records[0].S3.Object.Key, "elasticloadbalancing") {
			return "elb"
		}
		return "s3"
	}
	log.Fatalf("Could not extract event type")
	return ""
}

func ExtractLogs(data interface{}) []ingest.Log {
	logs := []ingest.Log{}
	source := ParseEventType(data)

	if debug {
		json, _ := json.Marshal(data)
		fmt.Printf("Event Recieved: %s\n", string(json))
		fmt.Printf("Source: %s\n", source)
	}

	switch source {
	case "cloudwatch":
		cloudWatchEvent := convertToCloudWatchLogsEvent(data)
		logs = parseCloudWatchLogs(cloudWatchEvent)
	case "s3":
		s3Event := convertToS3Event(data)
		logs = parseS3logs(s3Event, getContentsFromS3Bucket)
	case "elb":
		s3Event := convertToS3Event(data)
		logs = parseELBlogs(s3Event, getContentsFromS3Bucket)
	}
	return logs
}

// Lambda handler
func handler(request interface{}) {
	logs := ExtractLogs(request)
	ScrubLogsWithRegex(logs)
	SendLogs(logs)
}

func main() {
	ExtractEnvironmentVariables()
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: false}
	lambda.Start(handler)
}
