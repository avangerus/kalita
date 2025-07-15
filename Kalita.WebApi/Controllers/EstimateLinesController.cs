// Kalita.WebApi/Controllers/EstimateLinesController.cs

using Microsoft.AspNetCore.Mvc;
using Kalita.Domain.Entities;
using Kalita.Application.Services;
using Kalita.WebApi.DTO;

namespace Kalita.WebApi.Controllers;

[ApiController]
[Route("api/estimates/{estimateId}/lines")]
public class EstimateLinesController : KalitaBaseController
{
    private readonly EstimateLineService _service;

    public EstimateLinesController(EstimateLineService service)
    {
        _service = service;
    }

    [HttpGet]
    public IActionResult GetAll(Guid estimateId)
    {
        var lines = _service.GetLinesByEstimate(estimateId);
        return Ok(lines);
    }


    [HttpGet("{id}")]
    public IActionResult GetOne(Guid estimateId, Guid id)
    {
        var line = _service.GetLine(id);
        if (line == null || line.EstimateId != estimateId) return NotFound();
        return Ok(line);
    }

    [HttpPost]
    public IActionResult Create(Guid estimateId, [FromBody] CreateEstimateLineRequest req)
    {
        var result = _service.CreateLine(estimateId, req.Name, req.Quantity, req.Price, req.UnitId);
        if (!result.Success) return BadRequest(result.Error);
        return Ok(result.Line);
    }




    [HttpPut("{id}")]
    public IActionResult Update(Guid estimateId, Guid id, [FromBody] UpdateEstimateLineRequest req)
    {
        var result = _service.UpdateLine(id, req.Name, req.Quantity, req.Price, req.UnitId);
        if (!result.Success) return BadRequest(result.Error);
        return Ok(result.Line);
    }

    [HttpDelete("{id}")]
    public IActionResult Delete(Guid estimateId, Guid id)
    {
        if (_service.DeleteLine(id))
            return Ok();
        return NotFound();
    }
}


