namespace Kalita.WebApi.DTO
{
    public class CreateContractorRequest
    {
        public string Name { get; set; } = "";
        public string? Inn { get; set; }
        public string? Kpp { get; set; }
        public string? Address { get; set; }
    }
}
