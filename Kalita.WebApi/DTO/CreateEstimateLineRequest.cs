using System;

namespace Kalita.WebApi.DTO
{
    public class CreateEstimateLineRequest
    {
        public string Name { get; set; } = "";
        public decimal Quantity { get; set; }
        public decimal Price { get; set; }
        public Guid? UnitId { get; set; } // Можно убрать, если справочник не используешь
    }
}