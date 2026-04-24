package provisioner_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"go-tf-provisioner/internal/provisioner"
)

var _ = Describe("ProvisionRequest.Validate", func() {
	DescribeTable("required fields",
		func(req provisioner.ProvisionRequest, wantField string) {
			err := req.Validate()
			Expect(err).To(HaveOccurred())
			var ve *provisioner.ValidationError
			Expect(errors.As(err, &ve)).To(BeTrue())
			Expect(ve.Msg).To(ContainSubstring(wantField))
		},
		Entry("missing customerId", provisioner.ProvisionRequest{
			ProductCode: "w", CompanyName: "A", ContactEmail: "a@b.c",
		}, "customerId"),
		Entry("missing productCode", provisioner.ProvisionRequest{
			CustomerID: "c", CompanyName: "A", ContactEmail: "a@b.c",
		}, "productCode"),
		Entry("missing companyName", provisioner.ProvisionRequest{
			CustomerID: "c", ProductCode: "w", ContactEmail: "a@b.c",
		}, "companyName"),
		Entry("missing contactEmail", provisioner.ProvisionRequest{
			CustomerID: "c", ProductCode: "w", CompanyName: "A",
		}, "contactEmail"),
	)

	It("rejects malformed email addresses", func() {
		req := provisioner.ProvisionRequest{
			CustomerID: "c", ProductCode: "w", CompanyName: "A", ContactEmail: "not-an-email",
		}
		err := req.Validate()
		Expect(err).To(HaveOccurred())
		var ve *provisioner.ValidationError
		Expect(errors.As(err, &ve)).To(BeTrue())
		Expect(ve.Msg).To(ContainSubstring("contactEmail"))
	})

	It("accepts a well-formed request", func() {
		req := provisioner.ProvisionRequest{
			CustomerID:   "c",
			ProductCode:  "w",
			CompanyName:  "A",
			ContactEmail: "a@b.c",
		}
		Expect(req.Validate()).To(Succeed())
	})
})
