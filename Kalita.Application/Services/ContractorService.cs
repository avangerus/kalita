using Kalita.Domain.Entities;
using Kalita.Application.Workflow;
using Kalita.Infrastructure.Persistence;
using Kalita.Application.DTO;

namespace Kalita.Application.Services
{

    public class ContractorService
    {
        private readonly AppDbContext _db;
        public ContractorService(AppDbContext db) => _db = db;

        public Contractor Create(string name, string? inn, string? kpp, string? address)
        {
            var c = new Contractor
            {
                Id = Guid.NewGuid(),
                Name = name,
                Inn = inn,
                Kpp = kpp,
                Address = address
            };
            _db.Contractors.Add(c);
            _db.SaveChanges();
            return c;
        }

        public List<Contractor> GetAll() => _db.Contractors.ToList();

        public Contractor? Get(Guid id) => _db.Contractors.FirstOrDefault(x => x.Id == id);

        public void Delete(Guid id)
        {
            var c = _db.Contractors.FirstOrDefault(x => x.Id == id);
            if (c != null)
            {
                _db.Contractors.Remove(c);
                _db.SaveChanges();
            }
        }
    }
}