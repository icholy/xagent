package awsmvm

import (
	"context"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"gotest.tools/v3/assert"
)

type fakeS3 struct {
	puts    map[string][]byte
	deletes []string
}

func (f *fakeS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	data, _ := io.ReadAll(in.Body)
	if f.puts == nil {
		f.puts = map[string][]byte{}
	}
	f.puts[*in.Bucket+"/"+*in.Key] = data
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3) DeleteObject(_ context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	f.deletes = append(f.deletes, *in.Bucket+"/"+*in.Key)
	return &s3.DeleteObjectOutput{}, nil
}

type fakePresigner struct{ url string }

func (f fakePresigner) PresignGetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.PresignOptions)) (*signedRequest, error) {
	return &signedRequest{URL: f.url + "/" + *in.Bucket + "/" + *in.Key}, nil
}

func TestS3StagerStageAndRemove(t *testing.T) {
	s3c := &fakeS3{}
	st := &S3Stager{client: s3c, presigner: fakePresigner{url: "https://presigned"}}
	ctx := context.Background()

	url, err := st.Stage(ctx, "bucket", "runner-1/7.json", []byte("payload"), 3600)
	assert.NilError(t, err)
	assert.Equal(t, string(s3c.puts["bucket/runner-1/7.json"]), "payload")
	assert.Equal(t, url, "https://presigned/bucket/runner-1/7.json")

	assert.NilError(t, st.Remove(ctx, "bucket", "runner-1/7.json"))
	assert.DeepEqual(t, s3c.deletes, []string{"bucket/runner-1/7.json"})
}
