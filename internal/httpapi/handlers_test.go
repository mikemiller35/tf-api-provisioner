package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	"go-tf-provisioner/internal/config"
	"go-tf-provisioner/internal/httpapi"
	httpmocks "go-tf-provisioner/internal/httpapi/mocks"
	"go-tf-provisioner/internal/provisioner"
	"go-tf-provisioner/internal/status"
)

func newTestServer(prov httpapi.ProvisionService) *httpapi.Server {
	return httpapi.NewServer(config.Config{Port: 0}, prov)
}

// doRequest drives the server's http.Handler directly, avoiding any network
// bind so tests stay hermetic.
func doRequest(s *httpapi.Server, method, target string, body []byte) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	s.Handler().ServeHTTP(rr, req)
	return rr
}

var _ = Describe("HTTP handlers", func() {
	var (
		ctrl    *gomock.Controller
		mockSvc *httpmocks.MockProvisionService
		srv     *httpapi.Server
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockSvc = httpmocks.NewMockProvisionService(ctrl)
		srv = newTestServer(mockSvc)
	})

	AfterEach(func() { ctrl.Finish() })

	Describe("POST /provision", func() {
		It("accepts a well-formed request and returns 202 with a job", func() {
			job := provisioner.Job{JobID: "j1", StatusKey: "customers/c1/widget.status.json", CustomerID: "c1"}
			mockSvc.EXPECT().
				Submit(gomock.Any(), provisioner.ProvisionRequest{
					CustomerID: "c1", ProductCode: "widget", CompanyName: "Acme", ContactEmail: "a@b.c",
				}).
				Return(job, nil)

			body, _ := json.Marshal(map[string]string{
				"customerId": "c1", "productCode": "widget", "companyName": "Acme", "contactEmail": "a@b.c",
			})
			rr := doRequest(srv, http.MethodPost, "/provision", body)
			Expect(rr.Code).To(Equal(http.StatusAccepted))

			var got provisioner.Job
			Expect(json.Unmarshal(rr.Body.Bytes(), &got)).To(Succeed())
			Expect(got).To(Equal(job))
		})

		It("returns 400 on malformed JSON", func() {
			rr := doRequest(srv, http.MethodPost, "/provision", []byte("not json"))
			Expect(rr.Code).To(Equal(http.StatusBadRequest))
		})

		It("returns 400 on validation error", func() {
			mockSvc.EXPECT().Submit(gomock.Any(), gomock.Any()).
				Return(provisioner.Job{}, &provisioner.ValidationError{Msg: "missing required fields: customerId"})

			body, _ := json.Marshal(map[string]string{"productCode": "widget"})
			rr := doRequest(srv, http.MethodPost, "/provision", body)
			Expect(rr.Code).To(Equal(http.StatusBadRequest))
			Expect(rr.Body.String()).To(ContainSubstring("customerId"))
		})

		It("returns 409 when a job is already in flight", func() {
			mockSvc.EXPECT().Submit(gomock.Any(), gomock.Any()).
				Return(provisioner.Job{}, provisioner.ErrJobInFlight)

			body, _ := json.Marshal(map[string]string{
				"customerId": "c1", "productCode": "widget", "companyName": "Acme", "contactEmail": "a@b.c",
			})
			rr := doRequest(srv, http.MethodPost, "/provision", body)
			Expect(rr.Code).To(Equal(http.StatusConflict))
		})

		It("rejects non-POST methods with 405", func() {
			rr := doRequest(srv, http.MethodGet, "/provision", nil)
			Expect(rr.Code).To(Equal(http.StatusMethodNotAllowed))
		})
	})

	Describe("GET /info", func() {
		It("returns 400 when customerId is missing", func() {
			rr := doRequest(srv, http.MethodGet, "/info", nil)
			Expect(rr.Code).To(Equal(http.StatusBadRequest))
		})

		It("returns 404 when no statuses are found", func() {
			mockSvc.EXPECT().List(gomock.Any(), "c1", "").Return(nil, nil)
			rr := doRequest(srv, http.MethodGet, "/info?customerId=c1", nil)
			Expect(rr.Code).To(Equal(http.StatusNotFound))
		})

		It("returns the list of statuses", func() {
			now := time.Now().UTC()
			statuses := []status.Status{{
				JobID: "j1", CustomerID: "c1", ProductCode: "widget",
				State: status.StateSucceeded, StartedAt: now,
			}}
			mockSvc.EXPECT().List(gomock.Any(), "c1", "").Return(statuses, nil)

			rr := doRequest(srv, http.MethodGet, "/info?customerId=c1", nil)
			Expect(rr.Code).To(Equal(http.StatusOK))
			var got []status.Status
			Expect(json.Unmarshal(rr.Body.Bytes(), &got)).To(Succeed())
			Expect(got).To(HaveLen(1))
			Expect(got[0].JobID).To(Equal("j1"))
		})

		It("passes productCode through as a filter", func() {
			mockSvc.EXPECT().List(gomock.Any(), "c1", "widget").Return([]status.Status{{
				JobID: "j1", CustomerID: "c1", ProductCode: "widget",
			}}, nil)

			rr := doRequest(srv, http.MethodGet, "/info?customerId=c1&productCode=widget", nil)
			Expect(rr.Code).To(Equal(http.StatusOK))
		})

		It("propagates context cancellation", func() {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			mockSvc.EXPECT().List(gomock.Any(), "c1", "").DoAndReturn(
				func(ctx context.Context, _, _ string) ([]status.Status, error) {
					return nil, ctx.Err()
				})

			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/info?customerId=c1", nil).WithContext(ctx)
			srv.Handler().ServeHTTP(rr, req)
			Expect(rr.Code).To(Equal(http.StatusInternalServerError))
		})
	})
})
