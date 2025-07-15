using Microsoft.AspNetCore.Mvc;
using Kalita.Application.Services;
using Kalita.Domain.Entities;
using Kalita.WebApi.DTO;

namespace Kalita.WebApi.Controllers;

[ApiController]
[Route("api/[controller]")]
public class ContractorsController : KalitaBaseController
{
    private readonly WorkflowEntityService _service;

    public ContractorsController(WorkflowEntityService service)
    {
        _service = service;
    }

    [HttpGet]
    public ActionResult<List<Contractor>> GetAll()
    {
        var contractors = _service.GetAll("Contractor").Cast<Contractor>().ToList();
        return Ok(contractors);
    }

    [HttpGet("{id}")]
    public ActionResult<Contractor?> Get(Guid id)
    {
        var contractor = _service.Get("Contractor", id) as Contractor;
        if (contractor == null) return NotFound();
        return Ok(contractor);
    }

    [HttpPost]
    public IActionResult Create([FromBody] CreateContractorRequest request)
    {
        var result = _service.CreateContractor(
            request.Name,
            request.Inn,
            request.Kpp,
            request.Address,
            request.Type,
            request.Phone,
            request.Email
        );
        if (!result.Success) return BadRequest(result.Error);
        return Ok(result.Contractor);
    }

    [HttpPut("{id}")]
    public IActionResult Update(Guid id, [FromBody] UpdateContractorRequest request)
    {
        var result = _service.UpdateContractor(
            id,
            request.Name,
            request.Inn,
            request.Kpp,
            request.Address,
            request.Type,
            request.Phone,
            request.Email
        );
        if (!result.Success) return BadRequest(result.Error);
        return Ok(result.Contractor);
    }

    [HttpDelete("{id}")]
    public IActionResult Delete(Guid id)
    {
        var result = _service.DeleteContractor(id);
        if (!result.Success) return BadRequest(result.Error);
        return Ok();
    }
}
