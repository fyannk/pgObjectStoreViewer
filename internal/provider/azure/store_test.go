// Copyright 2026 The ObjectStoreViewer Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package azure

import (
	"context"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/fyannk/pgObjectStoreViewer/internal/fault"
)

func TestSafeErrorRedactsAzureSDKDetails(t *testing.T) {
	t.Parallel()
	canary := "sas-sig-canary"
	for status, want := range map[int]fault.Category{401: fault.Authentication, 403: fault.Authorization, 404: fault.NotFound, 429: fault.Throttled, 500: fault.Unavailable} {
		err := safeError(context.Background(), &azcore.ResponseError{StatusCode: status, ErrorCode: canary})
		if fault.Categorize(err) != want || strings.Contains(err.Error(), canary) {
			t.Fatalf("status %d: %v", status, err)
		}
	}
}
