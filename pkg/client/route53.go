/*
Copyright 2020 The redhawk Authors

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

package client

import (
	"encoding/base64"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/sirupsen/logrus"

	"github.com/DevopsArtFactory/redhawk/pkg/constants"
	"github.com/DevopsArtFactory/redhawk/pkg/resource"
)

type Route53Client struct {
	Resource string
	Client   *route53.Route53
}

// GetResourceName returns resource name of client
func (r Route53Client) GetResourceName() string {
	return r.Resource
}

// NewRoute53Client creates a Route53Client
func NewRoute53Client(helper Helper) (Client, error) {
	session := GetAwsSession()
	return &Route53Client{
		Resource: constants.Route53ResourceName,
		Client:   GetRoute53ClientFn(session, helper.Region, helper.Credentials),
	}, nil
}

// GetRoute53ClientFn creates route53 client
func GetRoute53ClientFn(sess client.ConfigProvider, region string, creds *credentials.Credentials) *route53.Route53 {
	if creds == nil {
		return route53.New(sess, &aws.Config{Region: aws.String(region)})
	}
	return route53.New(sess, &aws.Config{Region: aws.String(region), Credentials: creds})
}

// Scan scans all data
func (r Route53Client) Scan() ([]resource.Resource, error) {
	var wg sync.WaitGroup
	var result []resource.Resource

	recordSets, err := r.GetRoute53List()
	if err != nil {
		return nil, err
	}

	if len(recordSets) == 0 {
		logrus.Debug("no record set found")
		return nil, nil
	}

	input := make(chan resource.Route53Resource)
	output := make(chan []resource.Resource)
	defer close(output)

	go func(input chan resource.Route53Resource, output chan []resource.Resource, wg *sync.WaitGroup) {
		var ret []resource.Resource
		for result := range input {
			ret = append(ret, result)
			wg.Done()
		}

		output <- ret
	}(input, output, &wg)

	f := func(rs *route53.ResourceRecordSet, ch chan resource.Route53Resource) {
		tmp := resource.Route53Resource{
			ResourceType: aws.String(constants.Route53ResourceName),
		}

		tmp.Name = rs.Name
		tmp.Type = rs.Type

		if rs.AliasTarget != nil {
			tmp.Alias = aws.Bool(true)
			logrus.Tracef("DNS route with alias found: %s", *rs.AliasTarget.DNSName)
			base64RouteTo := base64.StdEncoding.EncodeToString([]byte(*rs.AliasTarget.DNSName))

			logrus.Tracef("DNS route is base64 encoded: %s", base64RouteTo)
			tmp.RouteTo = aws.String(base64RouteTo)
		}

		if len(rs.ResourceRecords) > 0 {
			var routeTo []string
			for _, rr := range rs.ResourceRecords {
				routeTo = append(routeTo, *rr.Value)
			}
			rt := strings.Join(routeTo, constants.DefaultDelimiter)
			logrus.Tracef("DNS route with records found: %s", rt)
			// base64 encoding
			base64RouteTo := base64.StdEncoding.EncodeToString([]byte(rt))
			logrus.Tracef("DNS route is base64 encoded: %s", base64RouteTo)
			tmp.RouteTo = aws.String(base64RouteTo)
			tmp.Alias = aws.Bool(false)
		}

		tmp.TTL = rs.TTL

		ch <- tmp
	}

	logrus.Debugf("Record sets found: %d", len(recordSets))
	for _, rs := range recordSets {
		wg.Add(1)
		go f(rs, input)
	}

	wg.Wait()
	close(input)

	result = <-output
	logrus.Debugf("total valid Route53 data count: %d", len(result))

	return result, nil
}

// GetRoute53List get all record set in the account
func (r Route53Client) GetRoute53List() ([]*route53.ResourceRecordSet, error) {
	hostedZones, err := r.GetRoute53HostedZones()
	if err != nil {
		return nil, err
	}

	var ret []*route53.ResourceRecordSet
	for _, hz := range hostedZones {
		result, err := r.Client.ListResourceRecordSets(&route53.ListResourceRecordSetsInput{
			HostedZoneId: hz.Id,
		})

		ret = append(ret, result.ResourceRecordSets...)
		if err != nil {
			return nil, err
		}
	}

	return ret, nil
}

// GetRoute53HostedZones get all hosted zones in the account
func (r Route53Client) GetRoute53HostedZones() ([]*route53.HostedZone, error) {
	result, err := r.Client.ListHostedZones(&route53.ListHostedZonesInput{})
	if err != nil {
		return nil, err
	}

	return result.HostedZones, nil
}
