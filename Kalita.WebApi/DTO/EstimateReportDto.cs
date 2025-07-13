namespace Kalita.WebApi.DTO
{
    public class EstimateReportDto
    {
        public Guid EstimateId { get; set; }
        public decimal IncomeTotal { get; set; }
        public decimal OutcomeTotal { get; set; }
        public decimal InvoiceTotal { get; set; }
        public decimal ExpenseTotal { get; set; }
        public decimal Balance { get; set; }
    }
}