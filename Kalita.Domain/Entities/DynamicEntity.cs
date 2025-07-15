using System;

namespace Kalita.Domain.Entities
{
    public class DynamicEntity
    {
        public Guid Id { get; set; }
        public string TypeCode { get; set; } = "";
        public string JsonData { get; set; } = ""; // данные как сериализованный json
    }
}