namespace Kalita.Application.Models
{
    public class TransitionRequest
    {
        public string NextStatus { get; set; } = "";
        public string ActionCode { get; set; }
        public string? Comment { get; set; }
        public string UserRole { get; set; } = "role:TestUser"; // default, для MVP
    }
}
