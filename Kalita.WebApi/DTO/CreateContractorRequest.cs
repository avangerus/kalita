namespace Kalita.WebApi.DTO
{
    public class CreateContractorRequest
    {
        public string Name { get; set; }
        public string? Inn { get; set; }
        public string? Kpp { get; set; }
        public string? Address { get; set; }
        public string? Type { get; set; }
        public string? Phone { get; set; }
        public string? Email { get; set; }
    }
}
