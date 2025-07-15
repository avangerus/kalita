// // Kalita.Application/Services/EntityMetadataService.cs

// using System.Text.Json;
// using Kalita.Domain.Entities;

// public class EntityMetadataService
// {
//     private Dictionary<string, EntityType> _types = new();

//     public void LoadFromJson(string jsonPath)
//     {
//         var json = File.ReadAllText(jsonPath);
//         var list = JsonSerializer.Deserialize<List<EntityType>>(json) ?? new();
//         _types = list.ToDictionary(x => x.Code, x => x);
//     }

//     public List<EntityType> GetAllTypes() => _types.Values.ToList();

//     public EntityType? GetType(string code) =>
//         _types.TryGetValue(code, out var t) ? t : null;

//     public EntityType? GetTypeByCode(string code)
//     {
//         // Или твоя логика, если коллекция называется по-другому
//         return _types.TryGetValue(code, out var t) ? t : null;
//     }

// }





