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
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/sirupsen/logrus"

	"github.com/DevopsArtFactory/redhawk/pkg/constants"
	"github.com/DevopsArtFactory/redhawk/pkg/resource"
)

type S3Client struct {
	Resource string
	Client   *s3.S3
}

type BucketPolicy struct {
	Version   string      `json:"Version"`
	Statement []Statement `json:"Statement"`
}

type Statement struct {
	Effect    string      `json:"Effect"`
	Principal interface{} `json:"Principal"`
	Action    interface{} `json:"Action"`
	Resource  interface{} `json:"Resource"`
}

// GetResourceName returns resource name of client
func (s S3Client) GetResourceName() string {
	return s.Resource
}

// NewS3Client creates S3Client
func NewS3Client(helper Helper) (Client, error) {
	session := GetAwsSession()
	return &S3Client{
		Resource: constants.S3ResourceName,
		Client:   GetS3ClientFn(session, helper.Region, helper.Credentials),
	}, nil
}

// GetS3ClientFn creates s3 client
func GetS3ClientFn(sess client.ConfigProvider, region string, creds *credentials.Credentials) *s3.S3 {
	if creds == nil {
		return s3.New(sess, &aws.Config{Region: aws.String(region)})
	}
	return s3.New(sess, &aws.Config{Region: aws.String(region), Credentials: creds})
}

// Scan scans all data
func (s S3Client) Scan() ([]resource.Resource, error) {
	var result []resource.Resource
	var wg sync.WaitGroup

	logrus.Debug("Start scanning all buckets in the account")
	buckets, err := s.GetBucketList()
	if err != nil {
		return nil, err
	}

	if len(buckets) == 0 {
		logrus.Debug("no bucket found")
		return nil, nil
	}

	input := make(chan *resource.S3Resource)
	output := make(chan []resource.Resource)
	defer close(output)

	go func(input chan *resource.S3Resource, output chan []resource.Resource, wg *sync.WaitGroup) {
		var ret []resource.Resource
		for result := range input {
			if result != nil {
				ret = append(ret, *result)
			}
			wg.Done()
		}

		output <- ret
	}(input, output, &wg)

	f := func(bucket *s3.Bucket, ch chan *resource.S3Resource) {
		tmp := resource.S3Resource{
			ResourceType: aws.String(constants.S3ResourceName),
		}

		location, err := s.GetBucketLocation(*bucket.Name)
		if err != nil {
			ch <- nil
			return
		}

		if location == nil {
			location = aws.String(constants.DefaultRegion)
		}
		tmp.Region = location

		logging, err := s.GetBucketLogging(*bucket.Name)
		if err != nil {
			ch <- nil
			return
		}

		tmp.LoggingEnabled = aws.Bool(false)
		if logging != nil {
			tmp.LoggingEnabled = aws.Bool(true)
			tmp.LoggingBucket = logging.TargetBucket
		}

		tmp.Bucket = bucket.Name
		tmp.Created = bucket.CreationDate

		policy, err := s.GetBucketPolicy(*bucket.Name)
		if err != nil {
			tmp.Policy = nil
		} else {
			logrus.Tracef("Bucket policy found: %s", *tmp.Bucket)
			// base64 encoding
			base64Policy := base64.StdEncoding.EncodeToString([]byte(*policy))

			logrus.Tracef("Policy is base64 encoded: %s", base64Policy)
			tmp.Policy = &base64Policy
		}

		logrus.Tracef("new bucket is added: %s / %s", *tmp.Bucket, *tmp.Region)

		ch <- &tmp
	}

	logrus.Debugf("Buckets found: %d", len(buckets))
	for _, bucket := range buckets {
		wg.Add(1)
		go f(bucket, input)
	}

	wg.Wait()
	close(input)

	result = <-output
	logrus.Debugf("total valid s3 data count: %d", len(result))

	if len(result) == 0 {
		return nil, fmt.Errorf("no bucket exists in the region")
	}

	return result, nil
}

// GetSGList returns all security group list in the account
func (s S3Client) GetBucketList() ([]*s3.Bucket, error) {
	result, err := s.Client.ListBuckets(&s3.ListBucketsInput{})
	if err != nil {
		return nil, err
	}

	return result.Buckets, nil
}

// GetBucketLocation returns region of bucket
func (s S3Client) GetBucketLocation(bucket string) (*string, error) {
	result, err := s.Client.GetBucketLocation(&s3.GetBucketLocationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return nil, err
	}

	return result.LocationConstraint, nil
}

// GetBucketPolicy returns a bucket policy
func (s S3Client) GetBucketPolicy(bucket string) (*string, error) {
	result, err := s.Client.GetBucketPolicy(&s3.GetBucketPolicyInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return nil, err
	}

	return result.Policy, nil
}

// GetBucketLogging returns a bucket logging configuration
func (s S3Client) GetBucketLogging(bucket string) (*s3.LoggingEnabled, error) {
	result, err := s.Client.GetBucketLogging(&s3.GetBucketLoggingInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return nil, err
	}

	return result.LoggingEnabled, nil
}
