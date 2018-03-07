package mpawsalb

import (
	"errors"
	"flag"
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	mp "github.com/mackerelio/go-mackerel-plugin"
)

var graphdef = map[string]mp.Graphs{
	"alb.response_ext": {
		Label: "Response Time Percentile",
		Unit:  "float",
		Metrics: []mp.Metrics{
			{Name: "p99", Label: "p99"},
			{Name: "p95", Label: "p95"},
			{Name: "p90", Label: "p90"},
			{Name: "p50", Label: "p50"},
			{Name: "p10", Label: "p10"},
		},
	},
}

// Plugin is ALB plugin for mackerel.
type Plugin struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	CloudWatch      *cloudwatch.CloudWatch
	LBName          string
}

func (p *Plugin) prepare() error {
	sess, err := session.NewSession()
	if err != nil {
		return err
	}

	config := aws.NewConfig()
	if p.AccessKeyID != "" && p.SecretAccessKey != "" {
		config = config.WithCredentials(credentials.NewStaticCredentials(p.AccessKeyID, p.SecretAccessKey, ""))
	}
	if p.Region != "" {
		config = config.WithRegion(p.Region)
	}

	p.CloudWatch = cloudwatch.New(sess, config)

	return nil
}

func (p Plugin) getLastPercentile(stat map[string]float64, dimensions []*cloudwatch.Dimension, metricName string) error {
	now := time.Now()

	response, err := p.CloudWatch.GetMetricStatistics(&cloudwatch.GetMetricStatisticsInput{
		Dimensions: dimensions,
		StartTime:  aws.Time(now.Add(time.Duration(180) * time.Second * -1)), // 3 min (to fetch at least 1 data-point)
		EndTime:    aws.Time(now),
		MetricName: aws.String(metricName),
		Period:     aws.Int64(60),
		ExtendedStatistics: []*string{
			aws.String("p99"), aws.String("p95"), aws.String("p90"), aws.String("p50"), aws.String("p10"),
		},
		Namespace: aws.String("AWS/ApplicationELB"),
	})
	if err != nil {
		return err
	}

	datapoints := response.Datapoints
	if len(datapoints) == 0 {
		return errors.New("fetched no datapoints")
	}

	for _, percentile := range [...]string{"p99", "p95", "p90", "p50", "p10"} {
		latest := new(time.Time)
		var latestVal float64
		for _, dp := range datapoints {
			if dp.Timestamp.Before(*latest) {
				continue
			}

			latest = dp.Timestamp
			latestVal = *dp.ExtendedStatistics[percentile]
		}
		stat[percentile] = latestVal
	}

	return nil
}

// FetchMetrics fetch elb metrics
func (p Plugin) FetchMetrics() (map[string]float64, error) {
	stat := make(map[string]float64)

	glb := []*cloudwatch.Dimension{}
	if p.LBName != "" {
		g2 := &cloudwatch.Dimension{
			Name:  aws.String("LoadBalancer"),
			Value: aws.String(p.LBName),
		}
		glb = append(glb, g2)
	}

	if err := p.getLastPercentile(stat, glb, "TargetResponseTime"); err != nil {
		return nil, err
	}

	return stat, nil
}

// GraphDefinition for Mackerel
func (p Plugin) GraphDefinition() map[string]mp.Graphs {
	return graphdef
}

// Do the plugin
func Do() {
	optRegion := flag.String("region", "", "AWS Region")
	optLBName := flag.String("lbname", "", "ELB Name")
	optAccessKeyID := flag.String("access-key-id", "", "AWS Access Key ID")
	optSecretAccessKey := flag.String("secret-access-key", "", "AWS Secret Access Key")
	optTempfile := flag.String("tempfile", "", "Temp file name")
	flag.Parse()

	var alb Plugin

	if *optRegion == "" {
		ec2metadata := ec2metadata.New(session.New())
		if ec2metadata.Available() {
			alb.Region, _ = ec2metadata.Region()
		}
	} else {
		alb.Region = *optRegion
	}
	alb.AccessKeyID = *optAccessKeyID
	alb.SecretAccessKey = *optSecretAccessKey
	alb.LBName = *optLBName

	err := alb.prepare()
	if err != nil {
		log.Fatalln(err)
	}

	helper := mp.NewMackerelPlugin(alb)
	helper.Tempfile = *optTempfile

	helper.Run()
}
