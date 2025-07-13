
namespace Kalita.WebApi.DTO
{
    public class CreateInvoiceRequest
    {
        public string Name { get; set; }
        public decimal Amount { get; set; }
        public string Status { get; set; }
        public Guid EstimateId { get; set; }
        public List<Guid> LineIds { get; set; }
    }
}