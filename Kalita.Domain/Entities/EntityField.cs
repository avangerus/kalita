using System;

namespace Kalita.Domain.Entities
{
    public class EntityField
    {
        public Guid Id { get; set; }
        public Guid EntityTypeId { get; set; }
        public string Code { get; set; }           // "client", "budget", "deadline"
        public string DisplayName { get; set; }
        public string FieldType { get; set; }
        public bool IsRequired { get; set; }
        public string? LookupTypeCode { get; set; } // если это lookup, то "client" или "brand"
        public bool IsMultiValue { get; set; }
        public string? EnumOptions { get; set; }
        public string? DefaultValue { get; set; }
        public string? Description { get; set; }
    
}
}
