using System;
using System.Collections.Generic;

namespace Kalita.Domain.Entities
{
    public class DictionaryType
    {
        public Guid Id { get; set; }
        public string Code { get; set; } = ""; // "currency", "unit", ...
        public string Name { get; set; } = ""; // Отображаемое имя (например, "Валюты")
        public string? Description { get; set; }

        public ICollection<DictionaryItem> Items { get; set; } = new List<DictionaryItem>();
    }
}