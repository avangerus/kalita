using System;

namespace Kalita.Domain.Entities
{
    public class Contractor
    {
        public Guid Id { get; set; }
        public string Name { get; set; } = "";
        public string? Inn { get; set; }
        public string? Kpp { get; set; }
        public string? Address { get; set; }
        public string? Type { get; set; } // Юрлицо/ИП/физлицо, можно строкой для MVP
        public string? Phone { get; set; }
        public string? Email { get; set; }
        public bool IsActive { get; set; } = true;
    }
}