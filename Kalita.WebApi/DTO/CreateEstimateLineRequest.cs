namespace Kalita.WebApi.DTO
{
    public class CreateEstimateLineRequest
    {
        public string Name { get; set; } = "";
        public decimal Amount { get; set; }
        public EstimateLineType Type { get; set; }
    }
}
