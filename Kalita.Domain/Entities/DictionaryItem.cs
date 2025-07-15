using System;

namespace Kalita.Domain.Entities
{
    public class DictionaryItem
    {
        public Guid Id { get; set; }
        public Guid TypeId { get; set; }
        public DictionaryType? Type { get; set; }

        public string Value { get; set; } = "";    // Например, "Доллар США"
        public string Code { get; set; } = "";     // Например, "USD"
        public string? ExtraJson { get; set; }     // Для расширения: сортировка, внешний код и т.д.
        public bool IsActive { get; set; } = true;

        public Guid? ParentId { get; set; }
        public DictionaryItem? Parent { get; set; }
        public ICollection<DictionaryItem> Children { get; set; } = new List<DictionaryItem>();

    }
}